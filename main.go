package main

import (
	"context"
	"database/sql"
	"log"
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
		log.Fatal("WATCHDOG_JWT_SECRET environment variable is required")
	}

	// DATABASE_URL must be set to a valid PostgreSQL connection string.
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// Verify the connection is actually reachable before accepting traffic.
	if err := db.Ping(); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	// Load the initial configuration. If the file is absent the server should
	// not start, because provider routing depends on it.
	const configPath = "config.yaml"
	if err := config.Load(configPath); err != nil {
		log.Fatalf("load config: %v", err)
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
			log.Fatalf("listen and serve: %v", err)
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
		log.Fatalf("server shutdown: %v", err)
	}

	slog.Info("server stopped")
}
