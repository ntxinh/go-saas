package middleware

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ntxinh/go-saas/internal/shared/errs"
)

// MembershipChecker resolves a (orgID, userID) pair to the member's role.
// Satisfied by orgs.Service.IsMember via wire.go — shared/middleware may
// not import a feature package. Implementations run as the pool owner
// (bypassing RLS) because the check IS the tenant resolution step.
type MembershipChecker interface {
	IsMember(ctx context.Context, orgID, userID uuid.UUID) (role string, ok bool)
}

// Tenant resolves chi's {param} to an org, verifies the caller is a
// member, and stores TenantID + Role in the request context. Non-members
// and unparseable ids get a 404 — never 403, so existence is not leaked.
func Tenant(check MembershipChecker, param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserID(r.Context())
			if !ok {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			orgID, err := uuid.Parse(chi.URLParam(r, param))
			if err != nil {
				errs.Write(w, errs.ErrNotFound)
				return
			}
			role, ok := check.IsMember(r.Context(), orgID, userID)
			if !ok {
				errs.Write(w, errs.ErrNotFound)
				return
			}
			ctx := WithTenant(r.Context(), orgID)
			ctx = context.WithValue(ctx, ctxRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// WithRole stores the caller's org role in ctx (Tenant sets it; tests use it).
func WithRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, ctxRole, role)
}

// Role returns the caller's role in the resolved tenant ("owner"|
// "admin"|"member"). False outside a Tenant-scoped route.
func Role(ctx context.Context) (string, bool) {
	r, ok := ctx.Value(ctxRole).(string)
	return r, ok
}
