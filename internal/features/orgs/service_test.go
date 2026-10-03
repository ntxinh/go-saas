package orgs_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/testutil"
)

func newPool(t *testing.T) (*pgxpool.Pool, context.Context) {
	t.Helper()
	ctx := context.Background()
	pool, err := database.NewPool(ctx, testutil.Postgres(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool, ctx
}

func newService(t *testing.T) (*orgs.Service, *pgxpool.Pool, context.Context) {
	t.Helper()
	pool, ctx := newPool(t)
	return orgs.NewService(pool, nil, nil), pool, ctx
}

func pgid(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func addUser(t *testing.T, pool *pgxpool.Pool, ctx context.Context) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(ctx, "INSERT INTO users(id,email) VALUES($1,$2)", pgid(id), id.String()+"@t.c")
	require.NoError(t, err)
	return id
}

func TestCreateMakesOwnerMembership(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	assert.Equal(t, "Acme", org.Name)
	assert.Equal(t, "free", org.Plan)
	assert.NotEqual(t, uuid.Nil, org.TenantID)

	role, ok := svc.IsMember(ctx, org.TenantID, owner)
	assert.True(t, ok)
	assert.Equal(t, "owner", role)
}

func TestTwoUsersSeparateTenants(t *testing.T) {
	svc, pool, ctx := newService(t)
	a, b := addUser(t, pool, ctx), addUser(t, pool, ctx)

	orgA, err := svc.Create(ctx, a, "A Corp")
	require.NoError(t, err)
	orgB, err := svc.Create(ctx, b, "B Corp")
	require.NoError(t, err)

	// B is not a member of A.
	_, ok := svc.IsMember(ctx, orgA.TenantID, b)
	assert.False(t, ok)

	// Each user's org list shows only their own tenant.
	orgsA, err := svc.OrgsOf(ctx, a)
	require.NoError(t, err)
	require.Len(t, orgsA, 1)
	assert.Equal(t, orgA.TenantID, orgsA[0].TenantID)
	assert.Equal(t, "owner", orgsA[0].Role)

	orgsB, err := svc.OrgsOf(ctx, b)
	require.NoError(t, err)
	require.Len(t, orgsB, 1)
	assert.Equal(t, orgB.TenantID, orgsB[0].TenantID)
}

// TestRLSIsolation is the core guarantee: inside WithTenantTx as org A
// the app_user role sees ONLY A's rows in orgs and memberships, even
// though both exist physically.
func TestRLSIsolation(t *testing.T) {
	svc, pool, ctx := newService(t)
	a, b := addUser(t, pool, ctx), addUser(t, pool, ctx)
	orgA, err := svc.Create(ctx, a, "A Corp")
	require.NoError(t, err)
	_, err = svc.Create(ctx, b, "B Corp")
	require.NoError(t, err)

	require.NoError(t, database.WithTenantTx(ctx, pool, orgA.TenantID, func(tx pgx.Tx) error {
		var names []string
		rows, err := tx.Query(ctx, "SELECT name FROM orgs")
		require.NoError(t, err)
		for rows.Next() {
			var n string
			require.NoError(t, rows.Scan(&n))
			names = append(names, n)
		}
		require.NoError(t, rows.Err())
		assert.Equal(t, []string{"A Corp"}, names, "tenant A tx must not see org B")

		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM memberships").Scan(&n))
		assert.Equal(t, 1, n, "tenant A tx must see only A's memberships")
		return nil
	}))
}

func TestGetUpdateMembers(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner, m2 := addUser(t, pool, ctx), addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Old")
	require.NoError(t, err)

	got, err := svc.Get(ctx, org.TenantID)
	require.NoError(t, err)
	assert.Equal(t, "Old", got.Name)

	upd, err := svc.Update(ctx, org.TenantID, "New")
	require.NoError(t, err)
	assert.Equal(t, "New", upd.Name)

	require.NoError(t, svc.AddMember(ctx, org.TenantID, m2, "member"))
	members, err := svc.Members(ctx, org.TenantID)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.ElementsMatch(t,
		[]uuid.UUID{owner, m2},
		[]uuid.UUID{members[0].UserID, members[1].UserID})

	require.NoError(t, svc.ChangeRole(ctx, org.TenantID, m2, "admin"))
	role, ok := svc.IsMember(ctx, org.TenantID, m2)
	assert.True(t, ok)
	assert.Equal(t, "admin", role)

	require.NoError(t, svc.RemoveMember(ctx, org.TenantID, m2))
	_, ok = svc.IsMember(ctx, org.TenantID, m2)
	assert.False(t, ok)
}

func TestAddMemberDuplicateIs409(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner, m2 := addUser(t, pool, ctx), addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	require.NoError(t, svc.AddMember(ctx, org.TenantID, m2, "member"))
	err = svc.AddMember(ctx, org.TenantID, m2, "member")
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestAddMemberUnknownUserIsValidation(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	err = svc.AddMember(ctx, org.TenantID, uuid.New(), "member")
	assert.ErrorIs(t, err, errs.ErrValidation)
}

func TestRemoveLastOwnerIsValidation(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	err = svc.RemoveMember(ctx, org.TenantID, owner)
	assert.ErrorIs(t, err, errs.ErrValidation)

	// demoting the last owner is likewise refused
	err = svc.ChangeRole(ctx, org.TenantID, owner, "member")
	assert.ErrorIs(t, err, errs.ErrValidation)
}

func TestBadInputIsValidation(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	_, err = svc.Create(ctx, owner, "   ")
	assert.ErrorIs(t, err, errs.ErrValidation)

	m2 := addUser(t, pool, ctx)
	assert.ErrorIs(t, svc.AddMember(ctx, org.TenantID, m2, "superuser"), errs.ErrValidation)
	assert.ErrorIs(t, svc.ChangeRole(ctx, org.TenantID, m2, "superuser"), errs.ErrValidation)
}

func TestGetMissingOrgIsNotFound(t *testing.T) {
	svc, _, ctx := newService(t)
	_, err := svc.Get(ctx, uuid.New())
	assert.ErrorIs(t, err, errs.ErrNotFound)
}
