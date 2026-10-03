// Command worker consumes durable asynq tasks (email:invite,
// org:invite-expiry) enqueued by the API.
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
	// Flush spans after srv.Run returns (in-flight tasks done).
	defer func() { _ = shutdown(context.Background()) }()
	srv, mux, err := app.WireWorker(ctx, cfg, log)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		srv.Shutdown() // graceful: in-flight tasks finish
	}()
	log.Info("worker listening", "queues", "default")
	if err := srv.Run(mux); err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}
