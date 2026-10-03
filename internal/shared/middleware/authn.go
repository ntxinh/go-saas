package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/ntxinh/go-saas/internal/shared/errs"
)

// supabaseClaims is the Supabase access-token shape we consume.
type supabaseClaims struct {
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// Authn verifies a Bearer JWT against the Supabase JWKS (Supabase issues
// RS256 or ES256) and stores sub+email in the request context. Failures
// get a 401 problem+json.
func Authn(keys keyfunc.Keyfunc, issuer, audience string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !ok || raw == "" {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			var claims supabaseClaims
			if _, err := jwt.ParseWithClaims(raw, &claims, keys.Keyfunc,
				jwt.WithValidMethods([]string{"RS256", "ES256"}),
				jwt.WithIssuer(issuer),
				jwt.WithAudience(audience),
				jwt.WithExpirationRequired(),
			); err != nil {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			sub, err := uuid.Parse(claims.Subject)
			if err != nil {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), sub, claims.Email)))
		})
	}
}

// UpsertUser lazily syncs the authenticated user into the users table.
// Mount after Authn; sync failure answers 500.
//
// sync is a func (not *users.Service) because shared/middleware may not
// import a feature package — wire.go passes userSvc.Sync.
func UpsertUser(sync func(ctx context.Context, id uuid.UUID, email string) error) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := r.Context().Value(ctxUser).(identity)
			if !ok {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			if err := sync(r.Context(), u.ID, u.Email); err != nil {
				Logger(r.Context()).Error("user sync failed", "err", err)
				errs.Write(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
