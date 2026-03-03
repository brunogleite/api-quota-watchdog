// Package mock provides canned HTTP responses for registered providers when
// mock_enabled is set on the provider record. It allows callers to test the
// full proxy pipeline without hitting real upstream APIs.
package mock

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Responder generates provider-specific mock HTTP responses.
// It holds no state and is safe for concurrent use.
type Responder struct{}

// NewResponder constructs a Responder ready for use.
func NewResponder() *Responder {
	return &Responder{}
}

// Respond writes a canned JSON response appropriate for providerName.
// providerName matching is case-insensitive and whitespace-trimmed.
// Known providers: "openai", "twilio", "googlemaps".
// Any unrecognised provider receives a 200 OK with an empty JSON object.
func (m *Responder) Respond(w http.ResponseWriter, r *http.Request, providerName string) {
	name := strings.ToLower(strings.TrimSpace(providerName))

	var statusCode int
	var payload any

	switch name {
	case "openai":
		statusCode = http.StatusOK
		payload = map[string]any{
			"id": "chatcmpl-mock",
			"choices": []map[string]any{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": "mock response",
					},
				},
			},
		}
	case "twilio":
		statusCode = http.StatusCreated
		payload = map[string]string{
			"sid":    "SMmock000",
			"status": "queued",
		}
	case "googlemaps":
		statusCode = http.StatusOK
		payload = map[string]any{
			"status":  "OK",
			"results": []any{},
		}
	default:
		statusCode = http.StatusOK
		payload = map[string]any{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	// Encoding errors are discarded: if writing fails the connection is broken
	// and there is nothing further the handler can do.
	_ = json.NewEncoder(w).Encode(payload)
}
