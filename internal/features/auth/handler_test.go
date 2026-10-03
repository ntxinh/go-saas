package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/auth"
	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/middleware"
)

func mount(profile func(context.Context, uuid.UUID) (string, error)) *chi.Mux {
	f := auth.New(profile)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		f.RegisterRoutes(r)
	})
	return r
}

func TestMeReturnsProfile(t *testing.T) {
	sub := uuid.New()
	var gotID uuid.UUID
	r := mount(func(_ context.Context, id uuid.UUID) (string, error) {
		gotID = id
		return "ada@example.com", nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil).
		WithContext(middleware.WithUser(context.Background(), sub, "ada@example.com"))
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, sub, gotID)

	var body struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Orgs  []any  `json:"orgs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, sub.String(), body.ID)
	assert.Equal(t, "ada@example.com", body.Email)
	require.NotNil(t, body.Orgs, "orgs must be [] not null")
	assert.Empty(t, body.Orgs)
	assert.Contains(t, rec.Body.String(), `"orgs":[]`)
}

func TestMeWithoutIdentityIs401(t *testing.T) {
	r := mount(func(context.Context, uuid.UUID) (string, error) {
		return "x@y.z", nil
	})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/me", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func TestMeProfileNotFoundIs404(t *testing.T) {
	r := mount(func(context.Context, uuid.UUID) (string, error) {
		return "", errs.ErrNotFound
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil).
		WithContext(middleware.WithUser(context.Background(), uuid.New(), "a@b.c"))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestMeProfileErrorIs500(t *testing.T) {
	r := mount(func(context.Context, uuid.UUID) (string, error) {
		return "", errors.New("db down")
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil).
		WithContext(middleware.WithUser(context.Background(), uuid.New(), "a@b.c"))
	r.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
