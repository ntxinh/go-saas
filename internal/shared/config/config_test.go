package config_test

import (
	"testing"

	"github.com/exodia/go-saas/internal/shared/config"
	"github.com/stretchr/testify/assert"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := config.Load()
	assert.Error(t, err)
}

func TestDeriveJWKS(t *testing.T) {
	c := &config.Config{SupabaseURL: "https://x.supabase.co"}
	assert.Equal(t, "https://x.supabase.co/auth/v1/.well-known/jwks.json", c.JWKsURL())
}
