// Package app is the single composition root: config → pool → keyfunc →
// cipher → services → features → router. Flat function calls, no DI
// framework.
package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/exodia/go-saas/internal/features/auth"
	"github.com/exodia/go-saas/internal/features/users"
	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/security"
	"github.com/exodia/go-saas/internal/shared/server"
)

// Wire builds the application router. ctx bounds the JWKS refresh
// goroutine's lifetime.
func Wire(ctx context.Context, cfg *config.Config, log *slog.Logger) (*chi.Mux, error) {
	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	keys, err := keyfunc.NewDefaultCtx(ctx, []string{cfg.JWKsURL()})
	if err != nil {
		return nil, fmt.Errorf("app: jwks %s: %w", cfg.JWKsURL(), err)
	}

	// PII_KEY is optional in dev: without it, PII fields decrypt to "".
	var cipher *security.Cipher
	if cfg.PIIKey != "" {
		cipher, err = security.New(cfg.PIIKey)
		if err != nil {
			return nil, fmt.Errorf("app: %w", err)
		}
	}

	userSvc := users.NewService(users.NewRepo(pool), cipher)
	authFeat := auth.New(func(ctx context.Context, id uuid.UUID) (string, error) {
		p, err := userSvc.Profile(ctx, id)
		return p.Email, err
	})

	r := server.New(cfg, log)
	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.Authn(keys, cfg.Issuer(), "authenticated"))
		r.Use(middleware.UpsertUser(userSvc.Sync))
		authFeat.RegisterRoutes(r)
	})
	return r, nil
}
