package database_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/testutil"
)

func TestTenantTxSetsRoleAndGUC(t *testing.T) {
	url := testutil.Postgres(t)
	pool, err := database.NewPool(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()

	org := uuid.New()
	var role, guc string
	require.NoError(t, database.WithTenantTx(context.Background(), pool, org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT current_user, current_setting('app.current_tenant', true)`).Scan(&role, &guc)
	}))
	assert.Equal(t, "app_user", role)
	assert.Equal(t, org.String(), guc)
}

func TestWithTxRollback(t *testing.T) {
	url := testutil.Postgres(t)
	pool, err := database.NewPool(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()

	err = database.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(context.Background(), `INSERT INTO users(id,email) VALUES($1,$2)`, uuid.New(), "a@b.c"); e != nil {
			return e
		}
		return errors.New("boom")
	})
	require.Error(t, err)

	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n))
	assert.Equal(t, 0, n)
}
