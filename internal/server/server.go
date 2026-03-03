// Package server wires together all HTTP handlers, middleware, and dependencies
// into a ready-to-serve HTTP server.
package server

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/brunogleite/api-quota-watchdog/internal/alert"
	"github.com/brunogleite/api-quota-watchdog/internal/handler"
	"github.com/brunogleite/api-quota-watchdog/internal/middleware"
	"github.com/brunogleite/api-quota-watchdog/internal/mock"
	"github.com/brunogleite/api-quota-watchdog/internal/proxy"
	"github.com/brunogleite/api-quota-watchdog/internal/quota"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// Server holds the HTTP multiplexer and all application-level dependencies.
type Server struct {
	mux              *http.ServeMux
	proxyHandler     *handler.ProxyHandler
	authHandler      *handler.AuthHandler
	providerHandler  *handler.ProviderHandler
	authMiddleware   func(http.Handler) http.Handler
}

// NewServer constructs a Server, wires all dependencies from the provided *sql.DB
// and jwtSecret, and registers routes.
//
// jwtSecret is passed in (not read from env here) so that main.go is the single
// point that reads environment variables for security-sensitive config.
// WATCHDOG_WEBHOOK_URL is optional and is still read internally since it is not
// a security-critical secret.
func NewServer(db *sql.DB, jwtSecret []byte) *Server {
	st := store.New(db)

	enforcer := quota.NewEnforcer(st)

	// WATCHDOG_WEBHOOK_URL is optional; if absent, the dispatcher logs errors
	// on dispatch attempts but does not crash.
	webhookURL := os.Getenv("WATCHDOG_WEBHOOK_URL")
	dispatcher := alert.NewDispatcher(webhookURL)

	fwd := proxy.New()
	mockR := mock.NewResponder()
	proxyH := handler.NewProxyHandler(fwd, enforcer, dispatcher, st, mockR)

	authH := handler.NewAuthHandler(st, jwtSecret)
	providerH := handler.NewProviderHandler(st)

	authMW := middleware.Auth(jwtSecret)

	s := &Server{
		mux:             http.NewServeMux(),
		proxyHandler:    proxyH,
		authHandler:     authH,
		providerHandler: providerH,
		authMiddleware:  authMW,
	}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler, delegating to the underlying mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
