package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/testutil"
)

func newRouter(t *testing.T) (*chi.Mux, *pgxpool.Pool) {
	t.Helper()
	pool, err := database.NewPool(context.Background(), testutil.Postgres(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		orgs.NewFeature(orgs.NewService(pool, nil, nil), nil).RegisterRoutes(r)
	})
	return r, pool
}

func do(t *testing.T, r http.Handler, user uuid.UUID, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body != "" {
		rd = strings.NewReader(body)
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd).
		WithContext(middleware.WithUser(context.Background(), user, user.String()+"@t.c"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func createOrg(t *testing.T, r http.Handler, user uuid.UUID, name string) string {
	t.Helper()
	rec := do(t, r, user, http.MethodPost, "/v1/orgs", `{"name":"`+name+`"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var body struct {
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.TenantID
}

func TestOrgLifecycle(t *testing.T) {
	r, pool := newRouter(t)
	owner, other := addUser(context.Background(), t, pool), addUser(context.Background(), t, pool)
	orgID := createOrg(t, r, owner, "Acme")

	// list shows the org
	rec := do(t, r, owner, http.MethodGet, "/v1/orgs", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []struct {
		TenantID string `json:"tenant_id"`
		Name     string `json:"name"`
		Role     string `json:"role"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "Acme", list[0].Name)
	assert.Equal(t, "owner", list[0].Role)

	// member GET
	rec = do(t, r, owner, http.MethodGet, "/v1/orgs/"+orgID, "")
	assert.Equal(t, http.StatusOK, rec.Code)

	// non-member GET → 404 (no existence leak)
	rec = do(t, r, other, http.MethodGet, "/v1/orgs/"+orgID, "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// PATCH name
	rec = do(t, r, owner, http.MethodPatch, "/v1/orgs/"+orgID, `{"name":"Acme 2"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Acme 2")

	// add member, list members
	rec = do(t, r, owner, http.MethodPost, "/v1/orgs/"+orgID+"/members",
		`{"user_id":"`+other.String()+`","role":"member"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = do(t, r, owner, http.MethodGet, "/v1/orgs/"+orgID+"/members", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), other.String())

	// duplicate add → 409
	rec = do(t, r, owner, http.MethodPost, "/v1/orgs/"+orgID+"/members",
		`{"user_id":"`+other.String()+`","role":"member"}`)
	assert.Equal(t, http.StatusConflict, rec.Code)

	// the new member can now read the org
	rec = do(t, r, other, http.MethodGet, "/v1/orgs/"+orgID, "")
	assert.Equal(t, http.StatusOK, rec.Code)

	// change role, then remove
	rec = do(t, r, owner, http.MethodPatch, "/v1/orgs/"+orgID+"/members/"+other.String(),
		`{"role":"admin"}`)
	assert.Equal(t, http.StatusOK, rec.Code)

	rec = do(t, r, owner, http.MethodDelete, "/v1/orgs/"+orgID+"/members/"+other.String(), "")
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// removing the last owner → 422
	rec = do(t, r, owner, http.MethodDelete, "/v1/orgs/"+orgID+"/members/"+owner.String(), "")
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestCreateOrgValidation(t *testing.T) {
	r, pool := newRouter(t)
	owner := addUser(context.Background(), t, pool)

	rec := do(t, r, owner, http.MethodPost, "/v1/orgs", `{"name":""}`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)

	rec = do(t, r, owner, http.MethodPost, "/v1/orgs", `not json`)
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}
