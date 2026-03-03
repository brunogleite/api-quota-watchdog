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
	"github.com/brunogleite/api-quota-watchdog/internal/proxy"
	"github.com/brunogleite/api-quota-watchdog/internal/quota"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// alertThreshold is the quota usage fraction at which a webhook alert fires.
const alertThreshold = 0.80

// ProxyHandler handles incoming proxy requests, enforces quotas, forwards the
// request to the upstream provider, and records usage.
type ProxyHandler struct {
	proxy  *proxy.Proxy
	quota  *quota.Enforcer
	alert  *alert.Dispatcher
	store  *store.Store
}

// NewProxyHandler constructs a ProxyHandler with the required dependencies.
func NewProxyHandler(p *proxy.Proxy, q *quota.Enforcer, a *alert.Dispatcher, s *store.Store) *ProxyHandler {
	return &ProxyHandler{
		proxy: p,
		quota: q,
		alert: a,
		store: s,
	}
}

// ServeProxy handles POST /proxy/{provider}/{path...}.
// It enforces quota, forwards the request, and records usage unconditionally.
// If quota recording fails, the error is logged but the response is not altered —
// proxy availability beats perfect accounting.
func (h *ProxyHandler) ServeProxy(w http.ResponseWriter, r *http.Request) {
	providerName := r.PathValue("provider")
	downstreamPath := r.PathValue("path")

	if providerName == "" {
		apperror.New(http.StatusBadRequest, "missing provider in path", nil).Write(w)
		return
	}

	// Fetch provider record including the stored API key.
	provider, err := h.store.GetProviderByName(r.Context(), providerName)
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

	// Forward the request. statusCode tracks the upstream response for recording.
	// We use a capturing ResponseWriter to observe the status code written by Forward.
	crw := &capturingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
	forwardErr := h.proxy.Forward(r.Context(), crw, r, provider, downstreamPath)
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
		"method", r.Method,
		"status", crw.statusCode,
		"latency_ms", latencyMs,
	)

	// Record usage unconditionally. serviceID is 0 for now — full service
	// identification requires auth claims inspection (future work).
	const serviceID int64 = 0
	if err := h.quota.Record(r.Context(), provider.ID, serviceID, r.Method, crw.statusCode, latencyMs); err != nil {
		// Log and continue — proxy availability beats perfect accounting.
		slog.Error("proxy: record usage failed",
			"provider", providerName,
			"err", err,
		)
	}

	// Check whether the threshold has just been crossed and fire an alert.
	// This is a fire-and-forget goroutine; it must not block the request path.
	//
	// Goroutine owner: ProxyHandler.ServeProxy. The goroutine is bounded by the
	// Dispatcher's HTTP client timeout and the context passed in. No additional
	// shutdown is required — the goroutine exits when Dispatch returns.
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
