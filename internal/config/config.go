// Package config handles loading and hot-reloading of the YAML configuration file.
//
// Hot-reload strategy: os.Stat + time.Ticker.
// Justification: fsnotify is not in the approved dependency list. os.Stat polling
// on a sub-second ticker is sufficient for configuration files that change infrequently,
// adds zero dependencies, and is trivially testable without filesystem event mocking.
package config

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Provider holds the configuration for a single upstream API provider.
type Provider struct {
	Name         string `yaml:"name"`
	BaseURL      string `yaml:"base_url"`
	APIKeyHeader string `yaml:"api_key_header"`
}

// Config is the top-level application configuration structure.
type Config struct {
	Providers []Provider `yaml:"providers"`
}

// cfg is the package-level config state, guarded by mu.
// It is the only package-level variable permitted in this package.
var (
	mu  sync.RWMutex
	cfg Config
)

// Load reads the YAML file at path, parses it, and stores the result.
// It must be called once during startup before Get is used.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var c Config
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		return err
	}

	mu.Lock()
	cfg = c
	mu.Unlock()
	return nil
}

// Get returns a point-in-time snapshot of the current configuration.
// Always call Get() at the point of use; never hold the returned value
// across a request lifecycle.
func Get() Config {
	mu.RLock()
	defer mu.RUnlock()
	return cfg
}

// StartReloader polls the config file at path every interval and reloads it
// when the file's modification time has changed.
//
// Goroutine owner: main.go — the caller is responsible for providing a context
// that is cancelled on shutdown. The goroutine exits when ctx.Done() is closed.
func StartReloader(ctx context.Context, path string, interval time.Duration) {
	go func() {
		// Capture the modification time of the file as last seen.
		info, err := os.Stat(path)
		if err != nil {
			slog.Error("config reloader: initial stat failed", "path", path, "err", err)
			return
		}
		lastMod := info.ModTime()

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				slog.Info("config reloader: shutting down", "path", path)
				return
			case <-ticker.C:
				info, err := os.Stat(path)
				if err != nil {
					slog.Error("config reloader: stat failed", "path", path, "err", err)
					continue
				}
				if info.ModTime().Equal(lastMod) {
					continue
				}
				lastMod = info.ModTime()
				if err := Load(path); err != nil {
					slog.Error("config reloader: reload failed", "path", path, "err", err)
					continue
				}
				slog.Info("config reloader: config reloaded", "path", path)
			}
		}
	}()
}
