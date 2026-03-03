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
	// TODO: implement
	// SELECT id, name, base_url, api_key_header, api_key_value
	// FROM providers WHERE name = $1
	_ = name
	return Provider{}, nil
}

// RecordUsage inserts a usage record for a single proxied request.
// providerID and serviceID are foreign keys; method is the HTTP method;
// statusCode is the upstream HTTP response status; latencyMs is the round-trip
// latency in milliseconds.
func (s *Store) RecordUsage(ctx context.Context, providerID, serviceID int64, method string, statusCode int, latencyMs int64) error {
	// TODO: implement
	// INSERT INTO usage_records (provider_id, service_id, method, status_code, latency_ms, recorded_at)
	// VALUES ($1, $2, $3, $4, $5, NOW())
	_, _, _, _, _ = providerID, serviceID, method, statusCode, latencyMs
	return nil
}

// GetQuotaUsage returns the total request count (used) and the configured
// quota limit (limit) for a given provider within the current billing window.
// Returns sql.ErrNoRows if no quota configuration exists for the provider.
func (s *Store) GetQuotaUsage(ctx context.Context, providerID int64) (used int64, limit int64, err error) {
	// TODO: implement
	// SELECT COUNT(*) as used, q.request_limit as limit
	// FROM usage_records ur
	// JOIN quotas q ON q.provider_id = ur.provider_id
	// WHERE ur.provider_id = $1
	//   AND ur.recorded_at >= q.window_start
	// GROUP BY q.request_limit
	_ = providerID
	return 0, 0, nil
}

// GetQuotaThresholdCrossed returns true if the threshold alert for the given
// provider has already been fired in the current window, preventing duplicate alerts.
func (s *Store) GetQuotaThresholdCrossed(ctx context.Context, providerID int64) (bool, error) {
	// TODO: implement
	// SELECT threshold_crossed FROM quotas WHERE provider_id = $1
	_ = providerID
	return false, nil
}

// SetQuotaThresholdCrossed marks the threshold alert as fired for a provider
// so that subsequent requests over the threshold do not re-fire the alert.
func (s *Store) SetQuotaThresholdCrossed(ctx context.Context, providerID int64, crossed bool) error {
	// TODO: implement
	// UPDATE quotas SET threshold_crossed = $2 WHERE provider_id = $1
	_, _ = providerID, crossed
	return nil
}
