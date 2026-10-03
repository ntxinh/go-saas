package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-redis/redis_rate/v10"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/shared/middleware"
	"github.com/ntxinh/go-saas/internal/testutil"
)

func ratelimitSrv(rdb *redis.Client, next http.Handler) http.Handler {
	return middleware.RateLimit(redis_rate.NewLimiter(rdb))(next)
}

func okHandler(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func TestRateLimit_BurstDeniesEleventh(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	srv := ratelimitSrv(rdb, http.HandlerFunc(okHandler))
	for i := range 10 {
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/x", nil))
		require.Equal(t, http.StatusNoContent, rec.Code, "request %d", i+1)
	}

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/x", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}

func TestRateLimit_MinuteWindow(t *testing.T) {
	rdb, mr := testutil.Redis(t)
	srv := ratelimitSrv(rdb, http.HandlerFunc(okHandler))

	// 10/s keeps the burst window happy; ~144 requests in 14s must trip
	// the 120/minute limit.
	denied := 0
	for i := range 200 {
		if denied > 0 {
			break
		}
		if i > 0 && i%10 == 0 {
			mr.FastForward(time.Second)
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/x", nil))
		if rec.Code == http.StatusTooManyRequests {
			denied = i + 1
		}
	}
	assert.Positive(t, denied, "per-minute limit never fired")
}

func TestRateLimit_KeysByUserWhenAuthed(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	srv := ratelimitSrv(rdb, http.HandlerFunc(okHandler))
	user := uuid.New()

	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil).
		WithContext(middleware.WithUser(context.Background(), user, "u@x.y"))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)

	// redis_rate prefixes keys with "rate:".
	_, err := rdb.Get(context.Background(), "rate:rl:u:"+user.String()).Result()
	require.NoError(t, err, "expected user-keyed limiter entry")
}

func TestRateLimit_AuthedChainUsesUserKey(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	jwks, keys := newJWKS(t)
	limiter := redis_rate.NewLimiter(rdb)
	user := uuid.New()

	// Production chain from wire.go: ip bucket → authn → upsert → user bucket.
	mux := chi.NewRouter()
	mux.Use(middleware.RateLimit(limiter))
	mux.Use(newAuthn(t, jwks.URL))
	mux.Use(middleware.UpsertUser(func(context.Context, uuid.UUID, string) error { return nil }))
	mux.Use(middleware.RateLimit(limiter))
	mux.Get("/v1/x", okHandler)

	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.Header.Set("Authorization", "Bearer "+
		sign(t, jwt.SigningMethodES256, keys.ec, validClaims(user)))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	ctx := context.Background()
	_, err := rdb.Get(ctx, "rate:rl:u:"+user.String()).Result()
	require.NoError(t, err, "expected user-keyed limiter entry")
	_, err = rdb.Get(ctx, "rate:rl:ip:192.0.2.1").Result()
	require.NoError(t, err, "expected ip-keyed limiter entry too")
}

func TestRateLimit_XForwardedFor(t *testing.T) {
	rdb, _ := testutil.Redis(t)
	srv := ratelimitSrv(rdb, http.HandlerFunc(okHandler))

	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	_, err := rdb.Get(context.Background(), "rate:rl:ip:203.0.113.7").Result()
	require.NoError(t, err, "expected first XFF entry used as key")
}
