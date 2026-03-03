// Package middleware provides HTTP middleware for the API Quota Watchdog server.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/brunogleite/api-quota-watchdog/internal/apperror"
	"github.com/golang-jwt/jwt/v5"
)

// contextKey is an unexported type for context keys in this package.
// Using a named type prevents collisions with keys from other packages.
type contextKey string

// claimsKey is the context key under which validated JWT claims are stored.
const claimsKey contextKey = "claims"

// ClaimsFromContext retrieves the JWT claims injected by the Auth middleware.
// Returns the claims and true if present, or nil and false otherwise.
func ClaimsFromContext(ctx context.Context) (jwt.MapClaims, bool) {
	v := ctx.Value(claimsKey)
	if v == nil {
		return nil, false
	}
	claims, ok := v.(jwt.MapClaims)
	return claims, ok
}

// UserIDFromContext reads the "user_id" claim from the JWT stored in ctx and
// returns it as int64. JWT numeric claims are encoded as float64 by the library,
// so the conversion is explicit here. Returns (0, false) when the claim is
// absent or is not a numeric type.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	claims, ok := ClaimsFromContext(ctx)
	if !ok {
		return 0, false
	}
	raw, ok := claims["user_id"]
	if !ok {
		return 0, false
	}
	// golang-jwt/jwt encodes JSON numbers as float64 in MapClaims.
	f, ok := raw.(float64)
	if !ok {
		return 0, false
	}
	return int64(f), true
}

// Auth returns an HTTP middleware that validates HS256 JWT tokens supplied in
// the Authorization: Bearer <token> header. On validation failure it writes a
// 401 response via apperror and halts the handler chain. On success it injects
// the token claims into the request context and calls next.
func Auth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				apperror.New(http.StatusUnauthorized, "missing authorization header", nil).Write(w)
				return
			}

			const bearerPrefix = "Bearer "
			if !strings.HasPrefix(authHeader, bearerPrefix) {
				apperror.New(http.StatusUnauthorized, "authorization header must use Bearer scheme", nil).Write(w)
				return
			}

			tokenStr := strings.TrimPrefix(authHeader, bearerPrefix)

			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
				// Verify the signing method is HMAC (HS256) before accepting the key.
				// Justification for any: jwt.Keyfunc is a stdlib-style callback defined
				// by the jwt library; the key type is opaque until the algorithm is known.
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, apperror.New(http.StatusUnauthorized, "unexpected signing method", nil)
				}
				return secret, nil
			})
			if err != nil || !token.Valid {
				apperror.New(http.StatusUnauthorized, "invalid or expired token", err).Write(w)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
