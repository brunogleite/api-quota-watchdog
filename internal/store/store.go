// Package store contains all database access logic for API Quota Watchdog.
// No code outside this package may touch the *sql.DB handle.
// All queries use parameterized placeholders — never string concatenation.
package store

import (
	"context"
	"database/sql"
	"fmt"

	// lib/pq is the approved PostgreSQL driver. Imported for its side-effect
	// of registering the "postgres" driver with database/sql.
	_ "github.com/lib/pq"
)

// Provider represents an upstream API provider registered by a user.
// APIKeyValue was removed in migration 002: clients supply credentials in
// their forwarded request headers; the proxy no longer injects a stored key.
type Provider struct {
	ID           int64
	UserID       int64
	Name         string
	BaseURL      string
	APIKeyHeader string
	MockEnabled  bool
}

// User represents an authenticated tenant of the watchdog.
// PasswordHash is a bcrypt digest and must never be sent to callers.
type User struct {
	ID           int64
	Email        string
	PasswordHash string
}

// Store wraps a *sql.DB and exposes the complete data-access surface.
// It is the sole owner of the database connection pool reference.
type Store struct {
	db *sql.DB
}

// New constructs a Store backed by the given database connection pool.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetProviderByName returns the provider owned by userID with the given name.
// Returns sql.ErrNoRows if no such provider exists.
func (s *Store) GetProviderByName(ctx context.Context, userID int64, name string) (Provider, error) {
	const q = `
		SELECT id, user_id, name, base_url, api_key_header, mock_enabled
		FROM providers
		WHERE user_id = $1 AND name = $2`
	var p Provider
	err := s.db.QueryRowContext(ctx, q, userID, name).
		Scan(&p.ID, &p.UserID, &p.Name, &p.BaseURL, &p.APIKeyHeader, &p.MockEnabled)
	return p, err
}

// RecordUsage appends a usage record for a single proxied request.
// serviceID = 0 is stored as NULL (via NULLIF) when the caller is unidentified.
func (s *Store) RecordUsage(ctx context.Context, providerID, serviceID int64, method string, statusCode int, latencyMs int64) error {
	const q = `
		INSERT INTO usage_records (provider_id, service_id, method, status_code, latency_ms, recorded_at)
		VALUES ($1, NULLIF($2, 0), $3, $4, $5, NOW())`
	_, err := s.db.ExecContext(ctx, q, providerID, serviceID, method, statusCode, latencyMs)
	return err
}

// GetQuotaUsage returns the number of requests used and the configured limit
// for providerID within its current quota window.
// Returns (0, 0, nil) when no quota row exists for the provider.
func (s *Store) GetQuotaUsage(ctx context.Context, providerID int64) (used int64, limit int64, err error) {
	const q = `
		SELECT COUNT(ur.id), q.request_limit
		FROM quotas q
		LEFT JOIN usage_records ur
		       ON ur.provider_id = q.provider_id
		      AND ur.recorded_at >= q.window_start
		WHERE q.provider_id = $1
		GROUP BY q.request_limit`
	err = s.db.QueryRowContext(ctx, q, providerID).Scan(&used, &limit)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return used, limit, err
}

// GetQuotaThresholdCrossed returns whether the alert threshold has already
// been crossed for providerID in the current window.
// Returns (false, nil) when no quota row exists.
func (s *Store) GetQuotaThresholdCrossed(ctx context.Context, providerID int64) (bool, error) {
	const q = `SELECT threshold_crossed FROM quotas WHERE provider_id = $1`
	var crossed bool
	err := s.db.QueryRowContext(ctx, q, providerID).Scan(&crossed)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return crossed, err
}

// SetQuotaThresholdCrossed updates the edge-trigger flag for providerID.
// Set crossed=true when the threshold is first crossed; reset to false when
// the quota window is reset.
func (s *Store) SetQuotaThresholdCrossed(ctx context.Context, providerID int64, crossed bool) error {
	const q = `UPDATE quotas SET threshold_crossed = $2 WHERE provider_id = $1`
	_, err := s.db.ExecContext(ctx, q, providerID, crossed)
	return err
}

// CreateUser inserts a new user record and returns the assigned ID.
// passwordHash must already be a bcrypt digest — this function does not hash.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (int64, error) {
	const q = `INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`
	var id int64
	err := s.db.QueryRowContext(ctx, q, email, passwordHash).Scan(&id)
	return id, err
}

// GetUserByEmail returns the user record for the given email address.
// Returns sql.ErrNoRows if no user with that email exists.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (User, error) {
	const q = `SELECT id, email, password_hash FROM users WHERE email = $1`
	var u User
	err := s.db.QueryRowContext(ctx, q, email).Scan(&u.ID, &u.Email, &u.PasswordHash)
	return u, err
}

// CreateProvider inserts a new provider row for userID and, when requestLimit > 0,
// also inserts a corresponding quotas row — both within a single transaction.
// Returns the newly created Provider on success.
func (s *Store) CreateProvider(
	ctx context.Context,
	userID int64,
	name, baseURL, apiKeyHeader string,
	mockEnabled bool,
	requestLimit int64,
) (Provider, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Provider{}, fmt.Errorf("store: begin transaction: %w", err)
	}
	// Rollback is a no-op after a successful Commit.
	defer func() { _ = tx.Rollback() }()

	const insertProvider = `
		INSERT INTO providers (user_id, name, base_url, api_key_header, mock_enabled)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, name, base_url, api_key_header, mock_enabled`
	var p Provider
	err = tx.QueryRowContext(ctx, insertProvider, userID, name, baseURL, apiKeyHeader, mockEnabled).
		Scan(&p.ID, &p.UserID, &p.Name, &p.BaseURL, &p.APIKeyHeader, &p.MockEnabled)
	if err != nil {
		return Provider{}, fmt.Errorf("store: insert provider: %w", err)
	}

	if requestLimit > 0 {
		const insertQuota = `
			INSERT INTO quotas (provider_id, request_limit)
			VALUES ($1, $2)`
		if _, err = tx.ExecContext(ctx, insertQuota, p.ID, requestLimit); err != nil {
			return Provider{}, fmt.Errorf("store: insert quota: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return Provider{}, fmt.Errorf("store: commit transaction: %w", err)
	}
	return p, nil
}

// ListProviders returns all providers owned by userID, ordered by creation time.
// Returns an empty (non-nil) slice when the user has no providers.
func (s *Store) ListProviders(ctx context.Context, userID int64) ([]Provider, error) {
	const q = `
		SELECT id, user_id, name, base_url, api_key_header, mock_enabled
		FROM providers
		WHERE user_id = $1
		ORDER BY created_at ASC`
	rows, err := s.db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list providers: %w", err)
	}
	defer rows.Close()

	providers := make([]Provider, 0)
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.ID, &p.UserID, &p.Name, &p.BaseURL, &p.APIKeyHeader, &p.MockEnabled); err != nil {
			return nil, fmt.Errorf("store: scan provider row: %w", err)
		}
		providers = append(providers, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterate provider rows: %w", err)
	}
	return providers, nil
}

// DeleteProvider removes the provider identified by providerID, but only if it
// belongs to userID. The WHERE clause on user_id prevents cross-tenant deletion.
// Returns nil even when no row matched (idempotent delete).
func (s *Store) DeleteProvider(ctx context.Context, userID, providerID int64) error {
	const q = `DELETE FROM providers WHERE id = $1 AND user_id = $2`
	_, err := s.db.ExecContext(ctx, q, providerID, userID)
	return err
}
