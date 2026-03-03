// Package handler contains HTTP handler implementations, one file per resource group.
// Handlers are the sole layer permitted to construct apperror.AppError values —
// they translate internal errors into HTTP responses without leaking internals.
package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/brunogleite/api-quota-watchdog/internal/alert"
	"github.com/brunogleite/api-quota-watchdog/internal/apperror"
	"github.com/brunogleite/api-quota-watchdog/internal/middleware"
	"github.com/brunogleite/api-quota-watchdog/internal/mock"
	"github.com/brunogleite/api-quota-watchdog/internal/proxy"
	"github.com/brunogleite/api-quota-watchdog/internal/quota"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// alertThreshold is the quota usage fraction at which a webhook alert fires.
const alertThreshold = 0.80

// ProxyHandler handles incoming proxy requests, enforces quotas, forwards the
// request to the upstream provider (or the mock responder), and records usage.
type ProxyHandler struct {
	proxy  *proxy.Proxy
	quota  *quota.Enforcer
	alert  *alert.Dispatcher
	store  *store.Store
	mock   *mock.Responder
}

// NewProxyHandler constructs a ProxyHandler with the required dependencies.
func NewProxyHandler(p *proxy.Proxy, q *quota.Enforcer, a *alert.Dispatcher, s *store.Store, m *mock.Responder) *ProxyHandler {
	return &ProxyHandler{
		proxy: p,
		quota: q,
		alert: a,
		store: s,
		mock:  m,
	}
}

// ServeProxy handles POST /proxy/{provider}/{path...}.
// It enforces quota, then either delegates to the mock responder (when the
// provider has mock_enabled=true) or forwards to the real upstream. Usage is
// recorded unconditionally after both paths.
//
// If quota recording fails, the error is logged but the response is not altered —
// proxy availability beats perfect accounting.
func (h *ProxyHandler) ServeProxy(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	downstreamPath := r.PathValue("path")

	if providerName == "" {
		apperror.New(http.StatusBadRequest, "missing provider in path", nil).Write(w)
		return
	}

	// Extract the authenticated user ID. The Auth middleware guarantees this is
	// present on all routes that use it, but we check defensively.
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		apperror.New(http.StatusUnauthorized, "missing or invalid user identity", nil).Write(w)
		return
	}

	// Fetch the provider scoped to this user (enforces multi-tenant isolation).
	provider, err := h.store.GetProviderByName(r.Context(), userID, providerName)
	if err != nil {
		apperror.New(http.StatusNotFound, "unknown provider", err).Write(w)
		return
	}

	// Enforce quota before forwarding.
	if err := h.quota.Check(r.Context(), provider.ID); err != nil {
		if errors.Is(err, quota.ErrQuotaExceeded) {
			apperror.New(http.StatusTooManyRequests, "quota exceeded", err).Write(w)
			return
		}
		apperror.New(http.StatusInternalServerError, "quota check failed", err).Write(w)
		return
	}

	start := time.Now()

	// capturingResponseWriter observes the status code written by either the
	// mock responder or the real proxy so it can be included in usage records.
	crw := &capturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

	var forwardErr error
	if provider.MockEnabled {
		// Mock path: return a canned response without touching the real upstream.
		h.mock.Respond(crw, r, providerName)
	} else {
		// Real path: forward to the upstream provider.
		forwardErr = h.proxy.Forward(r.Context(), crw, r, provider, downstreamPath)
	}

	latencyMs := time.Since(start).Milliseconds()

	if forwardErr != nil {
		slog.Error("proxy: forward failed",
			"provider", providerName,
			"method", r.Method,
			"err", forwardErr,
		)
		apperror.New(http.StatusBadGateway, "upstream request failed", forwardErr).Write(w)
		// Fall through to record usage even on forward failure, per spec.
	}

	slog.Info("proxy: request forwarded",
		"provider", providerName,
		"user_id", userID,
		"method", r.Method,
		"status", crw.statusCode,
		"latency_ms", latencyMs,
		"mock", provider.MockEnabled,
	)

	// Record usage unconditionally. serviceID is 0 (stored as NULL) because
	// service-level identification is not part of the current auth claims.
	const serviceID int64 = 0
	if err := h.quota.Record(r.Context(), provider.ID, serviceID, r.Method, crw.statusCode, latencyMs); err != nil {
		// Log and continue — proxy availability beats perfect accounting.
		slog.Error("proxy: record usage failed", "provider", providerName, "err", err)
	}

	// Check whether the threshold has just been crossed and fire an alert.
	// This is a fire-and-forget goroutine; it must not block the request path.
	//
	// Goroutine owner: ProxyHandler.ServeProxy. The goroutine is bounded by the
	// Dispatcher's HTTP client timeout. No additional shutdown mechanism is needed.
	used, limit, err := h.store.GetQuotaUsage(r.Context(), provider.ID)
	if err == nil && quota.ThresholdExceeded(used, limit, alertThreshold) {
		alreadyCrossed, err := h.store.GetQuotaThresholdCrossed(r.Context(), provider.ID)
		if err == nil && !alreadyCrossed {
			if setErr := h.store.SetQuotaThresholdCrossed(r.Context(), provider.ID, true); setErr != nil {
				slog.Error("proxy: set threshold crossed", "provider", providerName, "err", setErr)
			} else {
				usedPct := float64(used) / float64(limit)
				go h.alert.Dispatch(r.Context(), providerName, usedPct)
			}
		}
	}
}

// capturingResponseWriter wraps http.ResponseWriter to capture the status code
// written by the proxy so it can be included in usage records and logs.
type capturingResponseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code before delegating to the underlying writer.
func (c *capturingResponseWriter) WriteHeader(code int) {
	if !c.wroteHeader {
		c.statusCode = code
		c.wroteHeader = true
		c.ResponseWriter.WriteHeader(code)
	}
}
