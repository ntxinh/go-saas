package orgs_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/features/orgs"
)

// Member → 403 on invite; promote to admin (RoleChanged → casbin g-line
// update) → 200. Exercises Tenant + RBAC on the real route stack.
func TestRBACRouteEnforcement(t *testing.T) {
	pool, ctx := newPool(t)
	ef, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	svc := orgs.NewService(pool, nil, ef)

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		orgs.NewFeature(svc, ef).RegisterRoutes(r)
	})

	owner := addUser(ctx, t, pool)
	member := addUser(ctx, t, pool)
	orgID := createOrg(t, r, owner, "Acme")

	rec := do(t, r, owner, http.MethodPost, "/v1/orgs/"+orgID+"/members",
		`{"user_id":"`+member.String()+`","role":"member"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// member can read but not invite
	rec = do(t, r, member, http.MethodGet, "/v1/orgs/"+orgID, "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, r, member, http.MethodPost, "/v1/orgs/"+orgID+"/invites",
		`{"email":"x@y.z","role":"member"}`)
	require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	// promote member → admin; the next request is allowed
	rec = do(t, r, owner, http.MethodPatch, "/v1/orgs/"+orgID+"/members/"+member.String(),
		`{"role":"admin"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = do(t, r, member, http.MethodPost, "/v1/orgs/"+orgID+"/invites",
		`{"email":"x@y.z","role":"member"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	// a user with no membership gets 404 from Tenant (existence hidden)
	stranger := addUser(ctx, t, pool)
	rec = do(t, r, stranger, http.MethodGet, "/v1/orgs/"+orgID, "")
	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}
