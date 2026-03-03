package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/brunogleite/api-quota-watchdog/internal/apperror"
	"github.com/brunogleite/api-quota-watchdog/internal/middleware"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// ProviderHandler handles CRUD operations for provider resources.
// Each operation is scoped to the authenticated user via UserIDFromContext.
type ProviderHandler struct {
	store *store.Store
}

// NewProviderHandler constructs a ProviderHandler backed by the given Store.
func NewProviderHandler(s *store.Store) *ProviderHandler {
	return &ProviderHandler{store: s}
}

// createProviderRequest is the JSON body expected by ServeCreate.
type createProviderRequest struct {
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	APIKeyHeader string `json:"api_key_header"`
	MockEnabled  bool   `json:"mock_enabled"`
	RequestLimit int64  `json:"request_limit"`
}

// ServeCreate handles POST /providers.
// It creates a new provider for the authenticated user and, if request_limit > 0,
// also creates the corresponding quota row — both atomically via store.CreateProvider.
func (h *ProviderHandler) ServeCreate(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		apperror.New(http.StatusUnauthorized, "missing or invalid user identity", nil).Write(w)
		return
	}

	var req createProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperror.New(http.StatusBadRequest, "invalid request body", err).Write(w)
		return
	}
	if req.Name == "" || req.BaseURL == "" {
		apperror.New(http.StatusBadRequest, "name and base_url are required", nil).Write(w)
		return
	}

	p, err := h.store.CreateProvider(r.Context(), userID, req.Name, req.BaseURL, req.APIKeyHeader, req.MockEnabled, req.RequestLimit)
	if err != nil {
		slog.Error("provider: create", "user_id", userID, "name", req.Name, "err", err)
		apperror.New(http.StatusConflict, "provider already exists or could not be created", err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

// ServeList handles GET /providers.
// It returns all providers owned by the authenticated user.
func (h *ProviderHandler) ServeList(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		apperror.New(http.StatusUnauthorized, "missing or invalid user identity", nil).Write(w)
		return
	}

	providers, err := h.store.ListProviders(r.Context(), userID)
	if err != nil {
		slog.Error("provider: list", "user_id", userID, "err", err)
		apperror.New(http.StatusInternalServerError, "could not list providers", err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(providers)
}

// ServeDelete handles DELETE /providers/{id}.
// It removes the provider only if it belongs to the authenticated user.
func (h *ProviderHandler) ServeDelete(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.UserIDFromContext(r.Context())
	if !ok {
		apperror.New(http.StatusUnauthorized, "missing or invalid user identity", nil).Write(w)
		return
	}

	rawID := r.PathValue("id")
	providerID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil {
		apperror.New(http.StatusBadRequest, "invalid provider id", err).Write(w)
		return
	}

	if err := h.store.DeleteProvider(r.Context(), userID, providerID); err != nil {
		slog.Error("provider: delete", "user_id", userID, "provider_id", providerID, "err", err)
		apperror.New(http.StatusInternalServerError, "could not delete provider", err).Write(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
