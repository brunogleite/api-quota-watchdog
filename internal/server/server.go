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
	"github.com/brunogleite/api-quota-watchdog/internal/proxy"
	"github.com/brunogleite/api-quota-watchdog/internal/quota"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// Server holds the HTTP multiplexer and all application-level dependencies.
type Server struct {
	mux          *http.ServeMux
	proxyHandler *handler.ProxyHandler
	authMiddleware func(http.Handler) http.Handler
}

// NewServer constructs a Server, wires all dependencies from the provided *sql.DB,
// and registers routes. The JWT secret and webhook URL are read from environment
// variables so that NewServer remains pure and testable.
func NewServer(db *sql.DB) *Server {
	// Instantiate the data access layer.
	st := store.New(db)

	// Instantiate the quota enforcer backed by the store.
	enforcer := quota.NewEnforcer(st)

	// Instantiate the alert dispatcher. WATCHDOG_WEBHOOK_URL is optional;
	// if absent, the dispatcher will simply log errors on dispatch attempts.
	webhookURL := os.Getenv("WATCHDOG_WEBHOOK_URL")
	dispatcher := alert.NewDispatcher(webhookURL)

	// Instantiate the proxy, which handles upstream forwarding.
	fwd := proxy.New(st)

	// Instantiate the proxy handler, composing all sub-dependencies.
	proxyH := handler.NewProxyHandler(fwd, enforcer, dispatcher, st)

	// The JWT secret is guaranteed non-empty at this point because main.go
	// refuses to start if WATCHDOG_JWT_SECRET is absent.
	jwtSecret := []byte(os.Getenv("WATCHDOG_JWT_SECRET"))
	authMW := middleware.Auth(jwtSecret)

	s := &Server{
		mux:            http.NewServeMux(),
		proxyHandler:   proxyH,
		authMiddleware: authMW,
	}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler, delegating to the underlying mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
