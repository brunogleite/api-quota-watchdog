// Package handler contains all HTTP handlers. It is the sole package permitted
// to construct AppError values — the translation boundary between internal errors
// and HTTP responses.
package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/brunogleite/api-quota-watchdog/internal/apperror"
	"github.com/brunogleite/api-quota-watchdog/internal/store"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost is the work factor for bcrypt password hashing.
// 12 is a reasonable default: secure against brute-force while keeping
// registration latency under ~300 ms on modern hardware.
const bcryptCost = 12

// AuthHandler handles user registration and login.
type AuthHandler struct {
	store     *store.Store
	jwtSecret []byte
}

// NewAuthHandler constructs an AuthHandler with the given store and JWT secret.
func NewAuthHandler(s *store.Store, jwtSecret []byte) *AuthHandler {
	return &AuthHandler{store: s, jwtSecret: jwtSecret}
}

// registerRequest is the JSON body expected by ServeRegister.
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// registerResponse is the JSON body returned on successful registration.
type registerResponse struct {
	Token  string `json:"token"`
	UserID int64  `json:"user_id"`
}

// ServeRegister handles POST /auth/register.
// It hashes the password with bcrypt, persists the user, and returns a signed JWT.
func (h *AuthHandler) ServeRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperror.New(http.StatusBadRequest, "invalid request body", err).Write(w)
		return
	}
	if req.Email == "" || req.Password == "" {
		apperror.New(http.StatusBadRequest, "email and password are required", nil).Write(w)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		slog.Error("auth: bcrypt generate", "err", err)
		apperror.New(http.StatusInternalServerError, "could not hash password", err).Write(w)
		return
	}

	userID, err := h.store.CreateUser(r.Context(), req.Email, string(hash))
	if err != nil {
		slog.Error("auth: create user", "email", req.Email, "err", err)
		apperror.New(http.StatusConflict, "email already registered", err).Write(w)
		return
	}

	token, err := h.issueToken(userID, req.Email)
	if err != nil {
		slog.Error("auth: issue token after register", "user_id", userID, "err", err)
		apperror.New(http.StatusInternalServerError, "could not issue token", err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(registerResponse{Token: token, UserID: userID})
}

// loginRequest is the JSON body expected by ServeLogin.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// loginResponse is the JSON body returned on successful login.
type loginResponse struct {
	Token  string `json:"token"`
	UserID int64  `json:"user_id"`
}

// ServeLogin handles POST /auth/login.
// It verifies the password against the stored bcrypt hash and returns a signed JWT.
func (h *AuthHandler) ServeLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperror.New(http.StatusBadRequest, "invalid request body", err).Write(w)
		return
	}
	if req.Email == "" || req.Password == "" {
		apperror.New(http.StatusBadRequest, "email and password are required", nil).Write(w)
		return
	}

	user, err := h.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Return the same message as a wrong password to avoid user enumeration.
			apperror.New(http.StatusUnauthorized, "invalid credentials", nil).Write(w)
			return
		}
		slog.Error("auth: get user by email", "email", req.Email, "err", err)
		apperror.New(http.StatusInternalServerError, "login failed", err).Write(w)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		apperror.New(http.StatusUnauthorized, "invalid credentials", nil).Write(w)
		return
	}

	token, err := h.issueToken(user.ID, user.Email)
	if err != nil {
		slog.Error("auth: issue token after login", "user_id", user.ID, "err", err)
		apperror.New(http.StatusInternalServerError, "could not issue token", err).Write(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(loginResponse{Token: token, UserID: user.ID})
}

// issueToken creates and signs an HS256 JWT containing user_id and email claims
// with a 24-hour expiry.
func (h *AuthHandler) issueToken(userID int64, email string) (string, error) {
	claims := jwt.MapClaims{
		"user_id": userID,
		"email":   email,
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(h.jwtSecret)
}
