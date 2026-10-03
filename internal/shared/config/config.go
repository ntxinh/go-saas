// Package config loads environment configuration with fail-fast validation.
package config

import (
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/caarlos0/env/v11"
)

// Config holds all runtime configuration for the service.
type Config struct {
	Port               int    `env:"PORT"              envDefault:"8080"`
	Env                string `env:"APP_ENV"           envDefault:"dev"`
	DatabaseURL        string `env:"DATABASE_URL"`
	RedisURL           string `env:"REDIS_URL"`
	SupabaseURL        string `env:"SUPABASE_URL"`
	SupabaseServiceKey string `env:"SUPABASE_SERVICE_KEY"`
	PIIKey             string `env:"PII_KEY"` // 64 hex chars = 32-byte AES-256-GCM key
	AppURL             string `env:"APP_URL"  envDefault:"http://localhost:8080"`
	OTLPEndpoint       string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
	OTLPHeaders        string `env:"OTEL_EXPORTER_OTLP_HEADERS"`
	ResendAPIKey       string `env:"RESEND_API_KEY"`
	SMTPAddr           string `env:"SMTP_ADDR" envDefault:"localhost:1025"`
	MailFrom           string `env:"MAIL_FROM" envDefault:"go-saas <no-reply@go-saas.local>"`
	CORSAllowedOrigins string `env:"CORS_ALLOWED_ORIGINS" envDefault:"http://localhost:3000"` // comma-separated

	piiKey []byte
}

// Load parses environment variables and validates required values.
func Load() (*Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	for _, req := range []struct {
		name, val string
	}{
		{"DATABASE_URL", c.DatabaseURL},
		{"REDIS_URL", c.RedisURL},
		{"SUPABASE_URL", c.SupabaseURL},
	} {
		if req.val == "" {
			return nil, fmt.Errorf("config: %w: %s is required", errRequired, req.name)
		}
	}
	if c.PIIKey != "" {
		key, err := hex.DecodeString(c.PIIKey)
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("config: %w: PII_KEY must be 64 hex chars (32 bytes)", errInvalid)
		}
		c.piiKey = key
	}
	return &c, nil
}

var (
	errRequired = errors.New("required value missing")
	errInvalid  = errors.New("invalid value")
)

// PIIKeyBytes returns the decoded 32-byte PII key, or nil if unset.
func (c *Config) PIIKeyBytes() []byte { return c.piiKey }

// JWKsURL returns the Supabase Auth JWKS endpoint.
func (c *Config) JWKsURL() string { return c.SupabaseURL + "/auth/v1/.well-known/jwks.json" }

// Issuer returns the Supabase Auth issuer claim value.
func (c *Config) Issuer() string { return c.SupabaseURL + "/auth/v1" }
