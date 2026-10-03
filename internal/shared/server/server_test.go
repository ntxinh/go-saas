package server_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/exodia/go-saas/internal/shared/server"
	"github.com/stretchr/testify/assert"
)

// A browser preflight must be answered with the configured ACAO and the
// custom headers the API actually uses — without this the SPA's authed,
// idempotent writes can never leave the browser.
func TestCORSPreflight(t *testing.T) {
	cfg := &config.Config{CORSAllowedOrigins: "http://localhost:3000, https://app.example.com"}
	r := server.New(cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	req := httptest.NewRequest(http.MethodOptions, "/v1/orgs", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization, Idempotency-Key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Idempotency-Key")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
	assert.Equal(t, "300", rec.Header().Get("Access-Control-Max-Age"))

	// An unlisted origin gets no ACAO.
	req2 := httptest.NewRequest(http.MethodOptions, "/v1/orgs", nil)
	req2.Header.Set("Origin", "https://evil.example.com")
	req2.Header.Set("Access-Control-Request-Method", "GET")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	assert.Empty(t, rec2.Header().Get("Access-Control-Allow-Origin"))
}
