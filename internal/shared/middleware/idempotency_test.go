package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/server"
	"github.com/exodia/go-saas/internal/testutil"
)

func authedPost(t *testing.T, user uuid.UUID, key string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/orgs", nil).
		WithContext(middleware.WithUser(context.Background(), user, "u@x.y"))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req
}

func TestIdempotency_ReplaysStoredResponse(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	user := uuid.New()
	var calls atomic.Int32
	srv := middleware.Idempotency(rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		server.WriteJSON(w, http.StatusCreated, map[string]string{"id": "org-1"})
	}))

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authedPost(t, user, "k1"))
	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Empty(t, rec.Header().Get("Idempotent-Replay"))

	replay := httptest.NewRecorder()
	srv.ServeHTTP(replay, authedPost(t, user, "k1"))
	assert.Equal(t, http.StatusCreated, replay.Code)
	assert.Equal(t, rec.Body.String(), replay.Body.String())
	assert.Equal(t, "true", replay.Header().Get("Idempotent-Replay"))
	assert.Equal(t, int32(1), calls.Load(), "handler must run once")
}

func TestIdempotency_InFlightReturns409(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	user := uuid.New()
	srv := middleware.Idempotency(rdb)(http.HandlerFunc(okHandler))

	require.NoError(t, rdb.Set(context.Background(),
		"idem:"+user.String()+":POST:/v1/orgs:busy", "processing", 0).Err())

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, authedPost(t, user, "busy"))
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func TestIdempotency_GetBypassesAndMissingKeyPasses(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	user := uuid.New()
	var calls atomic.Int32
	srv := middleware.Idempotency(rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	// GET never touches Redis even with the header present.
	get := httptest.NewRequest(http.MethodGet, "/v1/orgs", nil).
		WithContext(middleware.WithUser(context.Background(), user, "u@x.y"))
	get.Header.Set("Idempotency-Key", "k1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, get)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, int32(1), calls.Load())
	keys, err := rdb.Keys(context.Background(), "idem:*").Result()
	require.NoError(t, err)
	assert.Empty(t, keys)

	// POST without the header just runs.
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, authedPost(t, user, ""))
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, int32(2), calls.Load())
}

func TestIdempotency_DifferentKeysBothRun(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	user := uuid.New()
	var calls atomic.Int32
	srv := middleware.Idempotency(rdb)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, key := range []string{"a", "b"} {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, authedPost(t, user, key))
		assert.Equal(t, http.StatusNoContent, rec.Code)
	}
	assert.Equal(t, int32(2), calls.Load())
}
