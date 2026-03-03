package server

import "net/http"

// routes registers all HTTP routes on the server's mux.
// Uses Go 1.22+ method+path patterns for precise routing.
//
// Unauthenticated:
//   POST /auth/register  — create a new user account
//   POST /auth/login     — exchange credentials for a JWT
//
// Authenticated (require valid Bearer JWT):
//   POST   /providers        — register a new upstream provider
//   GET    /providers        — list all providers for the current user
//   DELETE /providers/{id}   — remove a provider by ID
//   POST   /proxy/{provider}/{path...} — proxy a request to a named provider
func (s *Server) routes() {
	// Unauthenticated auth endpoints.
	s.mux.HandleFunc("POST /auth/register", s.authHandler.ServeRegister)
	s.mux.HandleFunc("POST /auth/login", s.authHandler.ServeLogin)

	// Authenticated provider management endpoints.
	s.mux.Handle("POST /providers",
		s.authMiddleware(http.HandlerFunc(s.providerHandler.ServeCreate)),
	)
	s.mux.Handle("GET /providers",
		s.authMiddleware(http.HandlerFunc(s.providerHandler.ServeList)),
	)
	s.mux.Handle("DELETE /providers/{id}",
		s.authMiddleware(http.HandlerFunc(s.providerHandler.ServeDelete)),
	)

	// Authenticated proxy endpoint.
	s.mux.Handle("POST /proxy/{provider}/{path...}",
		s.authMiddleware(http.HandlerFunc(s.proxyHandler.ServeProxy)),
	)
}
