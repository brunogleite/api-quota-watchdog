// Package quota provides quota enforcement and usage recording for proxied requests.
// Threshold evaluation logic is kept as pure functions where possible to remain
// testable without a live database.
package quota

import (
	"context"
	"errors"
	"fmt"

	"github.com/brunogleite/api-quota-watchdog/internal/store"
)

// ErrQuotaExceeded is returned by Check when a provider has consumed its full quota.
var ErrQuotaExceeded = errors.New("quota exceeded")

// Enforcer checks and records quota usage for upstream providers.
type Enforcer struct {
	store *store.Store
}

// NewEnforcer constructs an Enforcer backed by the given Store.
func NewEnforcer(s *store.Store) *Enforcer {
	return &Enforcer{store: s}
}

// Check fetches the current quota usage for providerID and returns
// ErrQuotaExceeded if the limit has been reached or surpassed.
// The check is a synchronous DB read on every call — correctness over throughput.
func (e *Enforcer) Check(ctx context.Context, providerID int64) error {
	used, limit, err := e.store.GetQuotaUsage(ctx, providerID)
	if err != nil {
		return fmt.Errorf("quota check: get usage: %w", err)
	}
	if limit > 0 && used >= limit {
		return ErrQuotaExceeded
	}
	return nil
}

// Record delegates a usage entry to the store. It should be called after every
// proxied request, regardless of whether the upstream returned an error.
// If recording fails, the caller (handler) should log the error but must not
// fail the client response — proxy availability beats perfect accounting.
func (e *Enforcer) Record(ctx context.Context, providerID, serviceID int64, method string, statusCode int, latencyMs int64) error {
	return e.store.RecordUsage(ctx, providerID, serviceID, method, statusCode, latencyMs)
}

// ThresholdExceeded returns true if the ratio used/limit is at or above the
// given threshold (0.0–1.0). It is a pure function, testable without a database.
func ThresholdExceeded(used, limit int64, threshold float64) bool {
	if limit <= 0 {
		return false
	}
	return float64(used)/float64(limit) >= threshold
}
