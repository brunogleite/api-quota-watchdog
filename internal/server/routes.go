package server

import "net/http"

// routes registers all HTTP routes on the server's mux.
// Uses Go 1.22+ method+path patterns.
func (s *Server) routes() {
	s.mux.Handle("POST /proxy/{provider}/{path...}",
		s.authMiddleware(http.HandlerFunc(s.proxyHandler.ServeProxy)),
	)
}
