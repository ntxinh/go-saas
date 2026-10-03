package server_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/ntxinh/go-saas/internal/shared/config"
	"github.com/ntxinh/go-saas/internal/shared/server"
	"github.com/stretchr/testify/assert"
)

func TestHealthz(t *testing.T) {
	r := server.New(&config.Config{Port: 0}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	assert.Equal(t, 200, rec.Code)
}
