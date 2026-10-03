// Command api serves the go-saas HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ntxinh/go-saas/internal/app"
	"github.com/ntxinh/go-saas/internal/shared/config"
	"github.com/ntxinh/go-saas/internal/shared/database"
	"github.com/ntxinh/go-saas/internal/shared/otel"
	"github.com/ntxinh/go-saas/internal/shared/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(otel.LogHandler(slog.NewJSONHandler(os.Stdout, nil)))
	slog.SetDefault(log)
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	shutdown, err := otel.Setup(ctx, cfg)
	if err != nil {
		return err
	}
	// Flush spans after server.Run drains, before exit closes the pool.
	defer func() {
		fctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = shutdown(fctx)
	}()
	// Spec §7: run migrations at API boot under a pg advisory lock (safe
	// across replicas; the worker deliberately doesn't — it doesn't own
	// the schema lifecycle).
	if err := database.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	r, closeApp, err := app.Wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", fmt.Sprintf(":%d", cfg.Port), "env", cfg.Env)
	err = server.Run(ctx, r, fmt.Sprintf(":%d", cfg.Port))
	// HTTP has drained; now stop bus/scheduler and release resources in
	// spec §7 order (otel flush follows via the deferred shutdown above).
	closeApp()
	if err != nil {
		return err
	}
	log.Info("shutdown complete")
	return nil
}
