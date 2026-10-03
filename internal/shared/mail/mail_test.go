package mail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResendSendsPayload(t *testing.T) {
	var auth string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/emails", r.URL.Path)
		auth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer srv.Close()

	s := &resend{key: "rk_test", from: "a@b.c", baseURL: srv.URL + "/emails", client: srv.Client()}
	require.NoError(t, s.Send(context.Background(), "to@x.y", "Hi", "<p>x</p>"))

	assert.Equal(t, "Bearer rk_test", auth)
	assert.Equal(t, "a@b.c", body["from"])
	assert.Equal(t, []any{"to@x.y"}, body["to"])
	assert.Equal(t, "Hi", body["subject"])
	assert.Equal(t, "<p>x</p>", body["html"])
}

func TestResendErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad key"}`))
	}))
	defer srv.Close()

	s := &resend{key: "rk_bad", from: "a@b.c", baseURL: srv.URL + "/emails", client: srv.Client()}
	err := s.Send(context.Background(), "to@x.y", "Hi", "<p>x</p>")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
