package middleware

import (
	"net/http"

	"github.com/ntxinh/go-saas/internal/shared/errs"
)

// Enforcer decides whether sub may act on obj inside dom. Satisfied by
// orgs.Enforcer (casbin); shared/middleware may not import a feature
// package. ids are bare uuid strings — the enforcer owns any namespacing.
type Enforcer interface {
	Enforce(sub, dom, obj, act string) (bool, error)
}

// RBAC gates a request on the casbin enforcer. It must run inside the
// Tenant middleware (which resolves tenant + membership): the request's
// user id, tenant id, URL path and HTTP method become the enforce tuple.
// Denied → 403; a missing user → 401, a missing tenant → 403.
func RBAC(ef Enforcer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := UserID(r.Context())
			if !ok {
				errs.Write(w, errs.ErrUnauthorized)
				return
			}
			orgID, ok := TenantID(r.Context())
			if !ok {
				errs.Write(w, errs.ErrForbidden)
				return
			}
			allowed, err := ef.Enforce(userID.String(), orgID.String(), r.URL.Path, r.Method)
			if err != nil {
				errs.Write(w, err)
				return
			}
			if !allowed {
				errs.Write(w, errs.ErrForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
