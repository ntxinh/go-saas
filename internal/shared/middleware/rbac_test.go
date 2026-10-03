package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/exodia/go-saas/internal/shared/middleware"
)

type fakeEnforcer struct {
	sub, dom, obj, act string
	allow              bool
}

func (f *fakeEnforcer) Enforce(sub, dom, obj, act string) (bool, error) {
	f.sub, f.dom, f.obj, f.act = sub, dom, obj, act
	return f.allow, nil
}

func TestRBAC(t *testing.T) {
	user, org := uuid.New(), uuid.New()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	req := func() *http.Request {
		ctx := middleware.WithTenant(
			middleware.WithUser(context.Background(), user, "u@x.y"), org)
		return httptest.NewRequest(http.MethodPatch, "/v1/orgs/"+org.String(), nil).WithContext(ctx)
	}

	f := &fakeEnforcer{allow: true}
	rec := httptest.NewRecorder()
	middleware.RBAC(f)(next).ServeHTTP(rec, req())
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, user.String(), f.sub)
	assert.Equal(t, org.String(), f.dom)
	assert.Equal(t, "/v1/orgs/"+org.String(), f.obj)
	assert.Equal(t, http.MethodPatch, f.act)

	f.allow = false
	rec = httptest.NewRecorder()
	middleware.RBAC(f)(next).ServeHTTP(rec, req())
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// No tenant in ctx (mounted outside the Tenant middleware) → forbidden.
	rec = httptest.NewRecorder()
	middleware.RBAC(f)(next).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/orgs", nil).
			WithContext(middleware.WithUser(context.Background(), user, "u@x.y")))
	assert.Equal(t, http.StatusForbidden, rec.Code)

	// No authenticated user → unauthorized.
	rec = httptest.NewRecorder()
	middleware.RBAC(f)(next).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/v1/orgs/x", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
