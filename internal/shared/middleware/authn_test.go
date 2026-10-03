package middleware_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MicahParks/jwkset"
	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/shared/middleware"
)

const (
	testIssuer   = "https://proj.supabase.co/auth/v1"
	testAudience = "authenticated"
)

// jwksServer serves a JWKS document containing both keys' public halves.
type testKeys struct {
	ec *ecdsa.PrivateKey
	rs *rsa.PrivateKey
}

func newJWKS(t *testing.T) (*httptest.Server, testKeys) {
	t.Helper()
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	rs, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	mk := func(k any, kid string) jwkset.JWKMarshal {
		j, err := jwkset.NewJWKFromKey(k, jwkset.JWKOptions{
			Metadata: jwkset.JWKMetadataOptions{KID: kid},
		})
		require.NoError(t, err)
		return j.Marshal()
	}
	doc, err := json.Marshal(jwkset.JWKSMarshal{
		Keys: []jwkset.JWKMarshal{mk(ec.Public(), "ec-kid"), mk(rs.Public(), "rs-kid")},
	})
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc)
	}))
	t.Cleanup(srv.Close)
	return srv, testKeys{ec: ec, rs: rs}
}

func sign(t *testing.T, method jwt.SigningMethod, key any, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(method, claims)
	if method == jwt.SigningMethodES256 {
		tok.Header["kid"] = "ec-kid"
	} else {
		tok.Header["kid"] = "rs-kid"
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func validClaims(sub uuid.UUID) jwt.MapClaims {
	return jwt.MapClaims{
		"sub":   sub.String(),
		"email": "ada@example.com",
		"iss":   testIssuer,
		"aud":   testAudience,
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Unix(),
	}
}

func newAuthn(t *testing.T, jwksURL string) func(http.Handler) http.Handler {
	t.Helper()
	k, err := keyfunc.NewDefaultCtx(context.Background(), []string{jwksURL})
	require.NoError(t, err)
	return middleware.Authn(k, testIssuer, testAudience)
}

// probe records the user identity the handler sees in ctx.
func probe() (http.Handler, *uuid.UUID, *string) {
	var id uuid.UUID
	var email string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		id, ok = middleware.UserID(r.Context())
		if !ok {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		email, _ = middleware.Email(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return h, &id, &email
}

func TestAuthnAcceptsES256(t *testing.T) {
	srv, keys := newJWKS(t)
	sub := uuid.New()
	h, id, email := probe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+
		sign(t, jwt.SigningMethodES256, keys.ec, validClaims(sub)))
	newAuthn(t, srv.URL)(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, sub, *id)
	assert.Equal(t, "ada@example.com", *email)
}

func TestAuthnAcceptsRS256(t *testing.T) {
	srv, keys := newJWKS(t)
	sub := uuid.New()
	h, id, _ := probe()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+
		sign(t, jwt.SigningMethodRS256, keys.rs, validClaims(sub)))
	newAuthn(t, srv.URL)(h).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, sub, *id)
}

func TestAuthnRejects(t *testing.T) {
	srv, keys := newJWKS(t)
	sub := uuid.New()

	badKidTok := jwt.NewWithClaims(jwt.SigningMethodES256, validClaims(sub))
	badKidTok.Header["kid"] = "no-such-kid"
	badKid, err := badKidTok.SignedString(keys.ec)
	require.NoError(t, err)

	expired := validClaims(sub)
	expired["exp"] = time.Now().Add(-time.Hour).Unix()

	wrongIss := validClaims(sub)
	wrongIss["iss"] = "https://evil.example.com"

	wrongAud := validClaims(sub)
	wrongAud["aud"] = "service_role"

	hs, err := jwt.NewWithClaims(jwt.SigningMethodHS256, validClaims(sub)).
		SignedString([]byte("shared-secret"))
	require.NoError(t, err)

	cases := map[string]string{
		"no header":      "",
		"not bearer":     "Basic dXNlcjpwYXNz",
		"garbage":        "Bearer not.a.jwt",
		"unknown kid":    "Bearer " + badKid,
		"expired":        "Bearer " + sign(t, jwt.SigningMethodES256, keys.ec, expired),
		"wrong issuer":   "Bearer " + sign(t, jwt.SigningMethodES256, keys.ec, wrongIss),
		"wrong audience": "Bearer " + sign(t, jwt.SigningMethodES256, keys.ec, wrongAud),
		"HS256":          "Bearer " + hs,
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			h, _, _ := probe()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			newAuthn(t, srv.URL)(h).ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code)
			assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
		})
	}
}

func TestUpsertUserCallsSync(t *testing.T) {
	sub := uuid.New()
	var gotID uuid.UUID
	var gotEmail string
	sync := func(_ context.Context, id uuid.UUID, email string) error {
		gotID, gotEmail = id, email
		return nil
	}
	h := middleware.UpsertUser(sync)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(middleware.WithUser(context.Background(), sub, "ada@example.com"))
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, sub, gotID)
	assert.Equal(t, "ada@example.com", gotEmail)
}

func TestUpsertUserSyncErrorIs500(t *testing.T) {
	sync := func(context.Context, uuid.UUID, string) error { return assert.AnError }
	h := middleware.UpsertUser(sync)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(middleware.WithUser(context.Background(), uuid.New(), "a@b.c"))
	h.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
}
