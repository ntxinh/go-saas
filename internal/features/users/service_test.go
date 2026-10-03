package users_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/users"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/security"
	"github.com/exodia/go-saas/internal/testutil"
)

func newService(t *testing.T) (*users.Service, *pgxpool.Pool, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := database.NewPool(ctx, testutil.Postgres(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	cipher, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)

	return users.NewService(users.NewRepo(pool), cipher), pool, ctx
}

func pgid(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func TestSyncInsertsThenUpdates(t *testing.T) {
	svc, pool, ctx := newService(t)
	id := uuid.New()

	require.NoError(t, svc.Sync(ctx, id, "ada@example.com"))
	before, err := svc.Profile(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "ada@example.com", before.Email)

	require.NoError(t, svc.Sync(ctx, id, "ada2@example.com"))
	after, err := svc.Profile(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, "ada2@example.com", after.Email)

	// one row only — the upsert must not duplicate
	var n int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM users WHERE id = $1", pgid(id)).Scan(&n))
	assert.Equal(t, 1, n)
}

func TestProfileDecryptsPII(t *testing.T) {
	svc, pool, ctx := newService(t)
	id := uuid.New()
	require.NoError(t, svc.Sync(ctx, id, "ada@example.com"))

	cipher, err := security.New(strings.Repeat("ab", 32))
	require.NoError(t, err)
	name, err := cipher.Encrypt("Ada Lovelace")
	require.NoError(t, err)
	phone, err := cipher.Encrypt("+491512345678")
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		"UPDATE users SET display_name = $2, phone = $3 WHERE id = $1",
		pgid(id), name, phone)
	require.NoError(t, err)

	p, err := svc.Profile(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, id, p.ID)
	assert.Equal(t, "ada@example.com", p.Email)
	assert.Equal(t, "Ada Lovelace", p.DisplayName)
	assert.Equal(t, "+491512345678", p.Phone)
}

func TestProfileNullPII(t *testing.T) {
	svc, _, ctx := newService(t)
	id := uuid.New()
	require.NoError(t, svc.Sync(ctx, id, "ada@example.com"))

	p, err := svc.Profile(ctx, id)
	require.NoError(t, err)
	assert.Empty(t, p.DisplayName)
	assert.Empty(t, p.Phone)
}

func TestProfileNotFound(t *testing.T) {
	svc, _, ctx := newService(t)
	_, err := svc.Profile(ctx, uuid.New())
	assert.ErrorIs(t, err, errs.ErrNotFound)
}
