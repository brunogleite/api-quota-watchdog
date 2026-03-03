package main

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// lib/pq registers the "postgres" driver as a side effect.
	_ "github.com/lib/pq"

	"github.com/brunogleite/api-quota-watchdog/internal/config"
	"github.com/brunogleite/api-quota-watchdog/internal/server"
)

func main() {
	// Initialize structured JSON logging as the global default logger.
	// This is the only package-level side effect permitted outside of init().
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// The JWT secret is mandatory. Refuse to start if it is absent so that
	// the server never runs in an insecure state.
	jwtSecret := os.Getenv("WATCHDOG_JWT_SECRET")
	if jwtSecret == "" {
		slog.Error("WATCHDOG_JWT_SECRET environment variable is required")
		os.Exit(1)
	}

	// DATABASE_URL must be set to a valid PostgreSQL connection string.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		slog.Error("open database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// Verify the connection is actually reachable before accepting traffic.
	if err := db.Ping(); err != nil {
		slog.Error("ping database", "err", err)
		os.Exit(1)
	}

	// Load the initial configuration. If the file is absent the server should
	// not start, because provider routing depends on it.
	const configPath = "config.yaml"
	if err := config.Load(configPath); err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	// Start the hot-reload background goroutine.
	// Goroutine owner: main. Stopped by cancelling rootCtx on shutdown.
	rootCtx, rootCancel := context.WithCancel(context.Background())
	defer rootCancel()

	config.StartReloader(rootCtx, configPath, 15*time.Second)

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      server.NewServer(db),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Listen for OS termination signals so we can shut down gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start serving in a goroutine.
	// Goroutine owner: main. Stopped when srv.Shutdown is called below.
	go func() {
		slog.Info("server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen and serve", "err", err)
			os.Exit(1)
		}
	}()

	// Block until a termination signal is received.
	<-quit
	slog.Info("server shutting down")

	// Cancel the root context to stop the config reloader and any other
	// context-aware goroutines.
	rootCancel()

	// Give in-flight requests up to 30 seconds to complete.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server shutdown", "err", err)
		os.Exit(1)
	}

	slog.Info("server stopped")
}
