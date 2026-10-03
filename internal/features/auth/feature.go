// Package auth serves the authenticated user's own endpoints (/v1/me).
package auth

import (
	"context"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Org is the user's membership in a tenant, as embedded in /v1/me.
// Declared here (not imported from orgs) because features may not
// import each other — wire.go adapts orgs.Service.OrgsOf.
type Org struct {
	TenantID uuid.UUID `json:"tenant_id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
}

// Feature is the auth feature module. profile resolves the caller's
// account email; orgs lists their memberships. Both are funcs (not
// concrete services) because wire.go adapts them.
type Feature struct {
	profile func(ctx context.Context, id uuid.UUID) (string, error)
	orgs    func(ctx context.Context, id uuid.UUID) ([]Org, error)
}

// New builds the auth feature (profile + orgs seams are wired in app).
func New(
	profile func(ctx context.Context, id uuid.UUID) (string, error),
	orgs func(ctx context.Context, id uuid.UUID) ([]Org, error),
) *Feature {
	return &Feature{profile: profile, orgs: orgs}
}

// RegisterRoutes mounts the feature's routes on r. Callers mount under
// /v1 behind Authn+UpsertUser.
func (f *Feature) RegisterRoutes(r chi.Router) {
	r.Get("/me", f.me)
}
