// Command api serves the go-saas HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/exodia/go-saas/internal/app"
	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/otel"
	"github.com/exodia/go-saas/internal/shared/server"
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
	defer func() { _ = shutdown(context.Background()) }()
	r, err := app.Wire(ctx, cfg, log)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", fmt.Sprintf(":%d", cfg.Port), "env", cfg.Env)
	if err := server.Run(ctx, r, fmt.Sprintf(":%d", cfg.Port)); err != nil {
		return err
	}
	log.Info("shutdown complete")
	return nil
}
