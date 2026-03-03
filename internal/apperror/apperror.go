// Package apperror defines the application error type and its HTTP response helper.
// It is the sole translation boundary between internal errors and HTTP responses.
// Only handler/ constructs AppError values; store/, proxy/, and quota/ return plain error.
package apperror

import (
	"encoding/json"
	"net/http"
)

// AppError represents an error that can be safely communicated to HTTP clients.
// Code is the HTTP status code. Message is safe to expose to clients.
// Err is the internal cause and is never serialized into the response body.
type AppError struct {
	Code    int    // HTTP status code
	Message string // safe for clients
	Err     error  // internal cause, never exposed to clients
}

// Error implements the error interface, returning the internal cause string.
func (a *AppError) Error() string {
	if a.Err != nil {
		return a.Err.Error()
	}
	return a.Message
}

// Unwrap returns the wrapped internal error, enabling errors.Is and errors.As.
func (a *AppError) Unwrap() error {
	return a.Err
}

// New constructs an AppError with the given HTTP status code, client-safe message,
// and an internal cause error (may be nil).
func New(code int, message string, err error) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// errorResponse is the JSON shape written to the response body.
type errorResponse struct {
	Error string `json:"error"`
}

// Write writes a JSON error response with the AppError's HTTP status code and
// client-safe message. The internal Err field is never included in the output.
func (a *AppError) Write(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(a.Code)
	// Encoding errors are intentionally ignored here: if we cannot write the
	// error response, there is nothing further we can do at this boundary.
	_ = json.NewEncoder(w).Encode(errorResponse{Error: a.Message})
}
