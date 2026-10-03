// Package testutil provides shared test helpers. Integration helpers
// require a container runtime; on Podman export:
//
//	export DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock
//	export TESTCONTAINERS_RYUK_DISABLED=true
//	export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/run/user/$UID/podman/podman.sock
//
// (`make podman-env` prints them; `make test` sets the first two.)
package testutil

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/ntxinh/go-saas/internal/shared/database"
)

// Postgres starts a postgres:16-alpine container, runs all migrations,
// and returns its connection URL. Skips when TESTCONTAINERS=skip.
func Postgres(t *testing.T) string {
	t.Helper()
	if os.Getenv("TESTCONTAINERS") == "skip" {
		t.Skip("TESTCONTAINERS=skip")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ctr.Terminate(context.Background()) })

	raw, err := ctr.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Container connects as superuser; grant it app_user so
	// SET LOCAL ROLE works inside tenant transactions.
	u, err := url.Parse(raw)
	require.NoError(t, err)
	u.User = url.UserPassword("postgres", "postgres")

	require.NoError(t, database.Migrate(ctx, u.String()))
	return u.String()
}
