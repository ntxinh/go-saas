package orgs_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/testutil"
)

func newEnforcer(t *testing.T) (*orgs.Enforcer, *pgxpool.Pool) {
	t.Helper()
	pool, err := database.NewPool(context.Background(), testutil.Postgres(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	ef, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	return ef, pool
}

func TestEnforcerPolicyRoundTrip(t *testing.T) {
	ef, pool := newEnforcer(t)
	orgA := uuid.New().String()
	uid := uuid.New().String()
	require.NoError(t, ef.Grant(context.Background(), orgA, uid, "admin"))

	var count int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM casbin_rule WHERE ptype='g'`).Scan(&count))
	assert.Equal(t, 1, count)

	// A fresh enforcer sees the persisted g-line.
	ef2, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	ok, err := ef2.Enforce(uid, orgA, "/v1/orgs/x", "GET")
	require.NoError(t, err)
	assert.True(t, ok)
	ok, err = ef2.Enforce(uuid.New().String(), orgA, "/v1/orgs/x", "GET")
	require.NoError(t, err)
	assert.False(t, ok)

	// Revoke persists across a reload.
	require.NoError(t, ef2.Revoke(context.Background(), orgA, uid))
	ef3, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	ok, err = ef3.Enforce(uid, orgA, "/v1/orgs/x", "GET")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestEnforceRoleMatrix(t *testing.T) {
	ef, _ := newEnforcer(t)
	ctx := context.Background()
	orgA, orgB := uuid.New().String(), uuid.New().String()
	owner, admin, member := uuid.New().String(), uuid.New().String(), uuid.New().String()
	require.NoError(t, ef.Grant(ctx, orgA, owner, "owner"))
	require.NoError(t, ef.Grant(ctx, orgA, admin, "admin"))
	require.NoError(t, ef.Grant(ctx, orgA, member, "member"))
	obj := "/v1/orgs/" + orgA
	for _, tc := range []struct {
		sub, dom, obj, act string
		want               bool
	}{
		{owner, orgA, obj, "GET", true},
		{owner, orgA, obj, "PATCH", true},
		{owner, orgA, obj + "/invites", "POST", true},
		{admin, orgA, obj + "/invites", "POST", true},
		{admin, orgA, obj + "/members/" + member, "PATCH", true},
		{member, orgA, obj, "GET", true},
		{member, orgA, obj + "/members", "GET", true},
		{member, orgA, obj + "/invites", "POST", false},
		{member, orgA, obj, "PATCH", false},
		{member, orgA, obj + "/invites/" + uuid.New().String(), "DELETE", false},
		{uuid.New().String(), orgA, obj, "GET", false}, // non-member
		// member of A is nobody in B (Tenant mw 404s first; casbin is the belt)
		{member, orgB, "/v1/orgs/" + orgB, "GET", false},
	} {
		ok, err := ef.Enforce(tc.sub, tc.dom, tc.obj, tc.act)
		require.NoError(t, err)
		assert.Equal(t, tc.want, ok, "%s %s %s in %s", tc.sub, tc.act, tc.obj, tc.dom)
	}


	// Revoke flips a previous allow to deny.
	require.NoError(t, ef.Revoke(ctx, orgA, member))
	ok, err := ef.Enforce(member, orgA, obj, "GET")
	require.NoError(t, err)
	assert.False(t, ok)
}
