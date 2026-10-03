// Package server wires the chi router, middleware and graceful shutdown.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riandyrn/otelchi"
	"golang.org/x/sync/errgroup"

	"github.com/exodia/go-saas/internal/shared/config"
	appmw "github.com/exodia/go-saas/internal/shared/middleware"
)

// New builds the application router.
func New(_ *config.Config, log *slog.Logger) *chi.Mux {
	r := chi.NewRouter()
	// OTel outermost (otelchi wraps otelhttp, names spans by route
	// pattern); healthz excluded from traces.
	r.Use(otelchi.Middleware("api",
		otelchi.WithChiRoutes(r),
		otelchi.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/healthz" }),
	))
	r.Use(appmw.RequestID(log))
	r.Use(appmw.Recover())
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return r
}

// Run serves r on addr until ctx is cancelled, then drains for 10s.
func Run(ctx context.Context, r http.Handler, addr string) error {
	srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return srv.ListenAndServe() })
	g.Go(func() error {
		<-ctx.Done()
		sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		return srv.Shutdown(sctx)
	})
	err := g.Wait()
	if err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
