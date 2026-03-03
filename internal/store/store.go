// Package store owns all database interactions. No other package is permitted
// to hold a *sql.DB reference or execute SQL directly.
// All queries use parameterized placeholders ($1, $2, ...) — never string concatenation.
package store

import (
	"context"
	"database/sql"

	// lib/pq is the approved PostgreSQL driver. It is imported for its side-effect
	// of registering the "postgres" driver with database/sql.
	_ "github.com/lib/pq"
)

// Provider is the store-layer representation of an upstream API provider row.
// It intentionally duplicates config.Provider because the DB record includes
// the stored API key value and a surrogate primary key.
type Provider struct {
	ID           int64
	Name         string
	BaseURL      string
	APIKeyHeader string
	APIKeyValue  string
}

// Store holds the database connection pool and exposes all data access methods.
type Store struct {
	db *sql.DB
}

// New constructs a Store from an open *sql.DB connection pool.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// GetProviderByName fetches a provider record by its unique name.
// Returns sql.ErrNoRows if no matching provider exists.
func (s *Store) GetProviderByName(ctx context.Context, name string) (Provider, error) {
	const q = `
		SELECT id, name, base_url, api_key_header, api_key_value
		FROM providers
		WHERE name = $1`

	var p Provider
	err := s.db.QueryRowContext(ctx, q, name).Scan(
		&p.ID,
		&p.Name,
		&p.BaseURL,
		&p.APIKeyHeader,
		&p.APIKeyValue,
	)
	return p, err
}

// RecordUsage inserts a usage record for a single proxied request.
// providerID is a required foreign key. serviceID 0 is stored as NULL
// because the caller may not yet be identified via JWT claims.
// method is the HTTP method; statusCode is the upstream response status;
// latencyMs is the round-trip latency in milliseconds.
func (s *Store) RecordUsage(ctx context.Context, providerID, serviceID int64, method string, statusCode int, latencyMs int64) error {
	const q = `
		INSERT INTO usage_records (provider_id, service_id, method, status_code, latency_ms, recorded_at)
		VALUES ($1, NULLIF($2, 0), $3, $4, $5, NOW())`

	_, err := s.db.ExecContext(ctx, q, providerID, serviceID, method, statusCode, latencyMs)
	return err
}

// GetQuotaUsage returns the total request count (used) and the configured
// request limit for a given provider within the current billing window.
// If no quota row exists for the provider, used and limit are both 0 (no enforcement).
func (s *Store) GetQuotaUsage(ctx context.Context, providerID int64) (used int64, limit int64, err error) {
	const q = `
		SELECT COUNT(ur.id), q.request_limit
		FROM quotas q
		LEFT JOIN usage_records ur
			ON  ur.provider_id = q.provider_id
			AND ur.recorded_at >= q.window_start
		WHERE q.provider_id = $1
		GROUP BY q.request_limit`

	err = s.db.QueryRowContext(ctx, q, providerID).Scan(&used, &limit)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return used, limit, err
}

// GetQuotaThresholdCrossed returns true if the threshold alert for the given
// provider has already been fired in the current window, preventing duplicate alerts.
// Returns false if no quota row exists for the provider.
func (s *Store) GetQuotaThresholdCrossed(ctx context.Context, providerID int64) (bool, error) {
	const q = `SELECT threshold_crossed FROM quotas WHERE provider_id = $1`

	var crossed bool
	err := s.db.QueryRowContext(ctx, q, providerID).Scan(&crossed)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return crossed, err
}

// SetQuotaThresholdCrossed marks the threshold alert as fired (or resets it) for
// a provider so that subsequent requests over the threshold do not re-fire the alert.
func (s *Store) SetQuotaThresholdCrossed(ctx context.Context, providerID int64, crossed bool) error {
	const q = `UPDATE quotas SET threshold_crossed = $2 WHERE provider_id = $1`

	_, err := s.db.ExecContext(ctx, q, providerID, crossed)
	return err
}
