package middleware

import (
	"context"

	"github.com/google/uuid"
)

// identity is the authenticated user Authn stores in the request context.
type identity struct {
	ID    uuid.UUID
	Email string
}

// WithUser stores an authenticated identity in ctx. Authn calls it; tests
// use it to fake an authenticated request.
func WithUser(ctx context.Context, id uuid.UUID, email string) context.Context {
	return context.WithValue(ctx, ctxUser, identity{ID: id, Email: email})
}

// UserID returns the authenticated user's id (JWT sub).
func UserID(ctx context.Context) (uuid.UUID, bool) {
	u, ok := ctx.Value(ctxUser).(identity)
	return u.ID, ok
}

// Email returns the authenticated user's email claim.
func Email(ctx context.Context) (string, bool) {
	u, ok := ctx.Value(ctxUser).(identity)
	return u.Email, ok
}

// WithTenant stores the resolved tenant id in ctx (used by the tenant
// middleware in a later task).
func WithTenant(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxTenant, id)
}

// TenantID returns the tenant id resolved for this request.
func TenantID(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(ctxTenant).(uuid.UUID)
	return id, ok
}
