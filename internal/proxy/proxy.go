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
type Proxy struct {
	client *http.Client
	store  *store.Store
}

// New constructs a Proxy with a default HTTP client tuned for upstream API calls.
func New(s *store.Store) *Proxy {
	return &Proxy{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		store: s,
	}
}

// Forward clones the incoming request, injects the provider API key, strips
// hop-by-hop headers, executes the upstream request, and copies the response
// back to w. It returns an error if the upstream request fails at the transport
// level; upstream non-2xx status codes are forwarded as-is and are not errors.
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

	// Clone the request so we can safely modify headers without affecting the
	// original request or any shared state.
	outReq, err := http.NewRequestWithContext(ctx, r.Method, upstreamURL, r.Body)
	if err != nil {
		return fmt.Errorf("proxy: build upstream request: %w", err)
	}

	// Copy original headers into the outbound request before stripping.
	for key, vals := range r.Header {
		for _, v := range vals {
			outReq.Header.Add(key, v)
		}
	}

	// Strip hop-by-hop headers from the outbound request.
	stripHopByHop(outReq.Header)

	// Inject the provider's stored API key. This overwrites any key the caller
	// may have supplied, ensuring we always use the server-side credential.
	if provider.APIKeyHeader != "" && provider.APIKeyValue != "" {
		outReq.Header.Set(provider.APIKeyHeader, provider.APIKeyValue)
	}

	resp, err := p.client.Do(outReq)
	if err != nil {
		return fmt.Errorf("proxy: upstream request failed: %w", err)
	}
	defer resp.Body.Close()

	// Strip hop-by-hop headers from the upstream response before forwarding.
	stripHopByHop(resp.Header)

	// Copy upstream response headers to the downstream response writer.
	for key, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		// The status code has already been written; we can only log this.
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
