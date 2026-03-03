// Package alert dispatches webhook notifications when quota thresholds are crossed.
// Alerts are edge-triggered: they fire once when a threshold is first crossed,
// not on every subsequent request that remains over the threshold.
// Crossing state is tracked in the database, not in memory.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Dispatcher sends webhook alerts to a configured URL.
type Dispatcher struct {
	webhookURL string
	client     *http.Client
}

// NewDispatcher constructs a Dispatcher that will POST alerts to webhookURL.
func NewDispatcher(webhookURL string) *Dispatcher {
	return &Dispatcher{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// alertPayload is the JSON body sent to the webhook endpoint.
type alertPayload struct {
	ProviderName string  `json:"provider_name"`
	UsedPct      float64 `json:"used_pct"`
	Message      string  `json:"message"`
}

// Dispatch sends a quota threshold alert to the configured webhook URL.
// It is designed to be invoked as a goroutine by the caller (quota/ or handler/)
// so it never blocks the request path.
//
// Goroutine owner: the caller (handler.ProxyHandler.ServeProxy) launches this
// via `go d.Dispatch(ctx, ...)`. The goroutine is short-lived and bounded by the
// HTTP client timeout; no additional shutdown mechanism is required.
func (d *Dispatcher) Dispatch(ctx context.Context, providerName string, usedPct float64) {
	payload := alertPayload{
		ProviderName: providerName,
		UsedPct:      usedPct,
		Message:      fmt.Sprintf("quota threshold crossed: %.1f%% used", usedPct*100),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("alert: marshal payload", "provider", providerName, "err", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.webhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("alert: build request", "provider", providerName, "err", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		slog.Error("alert: dispatch failed", "provider", providerName, "err", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		slog.Error("alert: webhook returned error status",
			"provider", providerName,
			"status", resp.StatusCode,
		)
		return
	}

	slog.Info("alert: dispatched", "provider", providerName, "used_pct", usedPct)
}
