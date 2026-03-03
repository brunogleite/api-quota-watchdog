// Package proxy implements hand-rolled HTTP request forwarding.
// It explicitly does NOT use httputil.ReverseProxy per the project constraints.
package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// hopByHopHeaders lists headers that must be stripped from both the outbound
// request and the inbound upstream response per RFC 7230 §6.1.
var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// Proxy forwards HTTP requests to upstream providers and copies responses back.
// The store dependency was removed in the multi-tenant refactor (migration 002):
// api_key_value no longer exists on the provider; clients supply credentials via
// the forwarded request headers.
type Proxy struct {
	client *http.Client
}

// New constructs a Proxy with a default HTTP client tuned for upstream API calls.
func New() *Proxy {
	return &Proxy{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Forward clones the incoming request, strips hop-by-hop headers, executes the
// upstream request, and copies the response back to w. Client-supplied API key
// headers are forwarded as-is — the server no longer injects a stored credential.
//
// Returns an error only on transport-level failures. Upstream non-2xx status
// codes are forwarded unchanged and are not treated as errors.
//
// The caller is responsible for recording quota usage after Forward returns,
// regardless of the error value.
func (p *Proxy) Forward(ctx context.Context, w http.ResponseWriter, r *http.Request, provider store.Provider, downstreamPath string) error {
	// Build the upstream URL by combining the provider base URL with the
	// downstream path and preserving any query string from the original request.
	upstreamURL := provider.BaseURL + "/" + downstreamPath
	if r.URL.RawQuery != "" {
		upstreamURL += "?" + r.URL.RawQuery
	}

	outReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, r.Body)
	if err != nil {
		return fmt.Errorf("proxy: build upstream request: %w", err)
	}

	// Copy original headers — client-supplied credentials are forwarded as-is.
	for key, vals := range r.Header {
		for _, v := range vals {
			outReq.Header.Add(key, v)
		}
	}
	stripHopByHop(outReq.Header)

	resp, err := p.client.Do(outReq)
	if err != nil {
		return fmt.Errorf("proxy: upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	// Strip hop-by-hop headers from the upstream response before forwarding.
	stripHopByHop(resp.Header)
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		// The status code has already been written; only log at call site.
		return fmt.Errorf("proxy: copy response body: %w", err)
	}
	return nil
}

// stripHopByHop removes all hop-by-hop headers from h in place.
func stripHopByHop(h http.Header) {
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
	// Also honour the Connection header's own list of headers to remove.
	for _, field := range h.Values("Connection") {
		h.Del(field)
	}
}
