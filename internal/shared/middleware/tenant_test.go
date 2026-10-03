package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/ntxinh/go-saas/internal/shared/middleware"
)

type fakeChecker struct {
	role  string
	ok    bool
	gotO  uuid.UUID
	gotU  uuid.UUID
	calls int
}

func (f *fakeChecker) IsMember(_ context.Context, orgID, userID uuid.UUID) (string, bool) {
	f.calls++
	f.gotO, f.gotU = orgID, userID
	return f.role, f.ok
}

func mountTenant(c middleware.MembershipChecker) (*chi.Mux, *uuid.UUID, *string) {
	var gotTenant uuid.UUID
	var gotRole string
	r := chi.NewRouter()
	r.Route("/v1/orgs/{orgID}", func(r chi.Router) {
		r.Use(middleware.Tenant(c, "orgID"))
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			gotTenant, _ = middleware.TenantID(req.Context())
			gotRole, _ = middleware.Role(req.Context())
			w.WriteHeader(http.StatusNoContent)
		})
	})
	return r, &gotTenant, &gotRole
}

func TestTenantMemberPassesAndSetsCtx(t *testing.T) {
	orgID, userID := uuid.New(), uuid.New()
	c := &fakeChecker{role: "admin", ok: true}
	r, gotTenant, gotRole := mountTenant(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+orgID.String(), nil).
		WithContext(middleware.WithUser(context.Background(), userID, "a@b.c"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, 1, c.calls)
	assert.Equal(t, orgID, c.gotO)
	assert.Equal(t, userID, c.gotU)
	assert.Equal(t, orgID, *gotTenant)
	assert.Equal(t, "admin", *gotRole)
}

func TestTenantNonMemberIs404(t *testing.T) {
	c := &fakeChecker{ok: false}
	r, _, _ := mountTenant(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/"+uuid.New().String(), nil).
		WithContext(middleware.WithUser(context.Background(), uuid.New(), "a@b.c"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code, "non-member must not learn the org exists")
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func TestTenantBadOrgIDIs404(t *testing.T) {
	c := &fakeChecker{ok: true, role: "owner"}
	r, _, _ := mountTenant(c)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/orgs/not-a-uuid", nil).
		WithContext(middleware.WithUser(context.Background(), uuid.New(), "a@b.c"))
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Zero(t, c.calls, "must not hit the db on an unparseable id")
}

func TestTenantNoIdentityIs401(t *testing.T) {
	c := &fakeChecker{ok: true, role: "owner"}
	r, _, _ := mountTenant(c)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/orgs/"+uuid.New().String(), nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRoleEmptyCtx(t *testing.T) {
	role, ok := middleware.Role(context.Background())
	assert.False(t, ok)
	assert.Empty(t, role)
}
