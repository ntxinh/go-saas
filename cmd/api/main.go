// Command api serves the go-saas HTTP API.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	r := server.New(cfg, log)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Info("listening", "addr", fmt.Sprintf(":%d", cfg.Port), "env", cfg.Env)
	if err := server.Run(ctx, r, fmt.Sprintf(":%d", cfg.Port)); err != nil {
		return err
	}
	log.Info("shutdown complete")
	return nil
}
