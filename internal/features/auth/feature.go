// Package auth serves the authenticated user's own endpoints (/v1/me).
package auth

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Feature is the auth feature module. profile resolves the caller's
// account email; it is a func (not *users.Service) because features may
// not import each other — wire.go adapts users.Service.Profile.
type Feature struct {
	profile func(ctx context.Context, id uuid.UUID) (string, error)
}

func New(profile func(ctx context.Context, id uuid.UUID) (string, error)) *Feature {
	return &Feature{profile: profile}
}

// RegisterRoutes mounts the feature's routes on r. Callers mount under
// /v1 behind Authn+UpsertUser.
func (f *Feature) RegisterRoutes(r chi.Router) {
	r.Get("/me", f.me)
}
