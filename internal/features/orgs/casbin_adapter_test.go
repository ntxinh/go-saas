package orgs_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/features/orgs"
	"github.com/ntxinh/go-saas/internal/shared/database"
	"github.com/ntxinh/go-saas/internal/shared/errs"
	"github.com/ntxinh/go-saas/internal/testutil"
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

// failAuthz returns err on the named op; nil means succeed.
type failAuthz struct{ grantErr, revokeErr error }

func (f failAuthz) Grant(context.Context, string, string, string) error { return f.grantErr }
func (f failAuthz) Revoke(context.Context, string, string) error        { return f.revokeErr }

// A failing Authorizer must surface from membership writes — a swallowed
// revoke failure would leave a demoted user's stale g-line granting
// their old role indefinitely.
func TestMembershipWritesPropagateAuthzError(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner, member := addUser(ctx, t, pool), addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	sentinel := errors.New("casbin down")

	// grant failure: AddMember commits the row but reports the desync
	svc = orgs.NewService(pool, nil, failAuthz{grantErr: sentinel})
	err = svc.AddMember(ctx, org.TenantID, member, "member")
	require.ErrorIs(t, err, sentinel)

	// revoke failure: ChangeRole/RemoveMember commit then report it
	svc = orgs.NewService(pool, nil, failAuthz{revokeErr: sentinel})
	err = svc.ChangeRole(ctx, org.TenantID, member, "admin")
	require.ErrorIs(t, err, sentinel)
	err = svc.RemoveMember(ctx, org.TenantID, member)
	require.ErrorIs(t, err, sentinel)
}

// The stale-g-line bug: a RemoveMember whose revoke failed leaves
// "role:owner" in casbin_rule while the membership row is gone. Re-adding
// the user at a lesser role must clear it — Grant is set-semantics. The
// row delete is done via SQL to simulate the unhealable state without
// needing a casbin failure injection.
func TestReAddClearsStaleRole(t *testing.T) {
	pool, ctx := newPool(t)
	ef, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	svc := orgs.NewService(pool, nil, ef)
	u := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, u, "Acme")
	require.NoError(t, err)

	// Owner grants POST; row removed, g-line survives (as a failed
	// revoke would leave it).
	ok, err := ef.Enforce(u.String(), org.TenantID.String(), "/v1/orgs/x", "POST")
	require.NoError(t, err)
	require.True(t, ok)
	_, err = pool.Exec(ctx, `DELETE FROM memberships WHERE tenant_id=$1 AND user_id=$2`,
		pgid(org.TenantID), pgid(u))
	require.NoError(t, err)

	require.NoError(t, svc.AddMember(ctx, org.TenantID, u, "member"))

	// Set-semantics Grant dropped the stale role:owner line.
	ok, err = ef.Enforce(u.String(), org.TenantID.String(), "/v1/orgs/x", "POST")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = ef.Enforce(u.String(), org.TenantID.String(), "/v1/orgs/x", "GET")
	require.NoError(t, err)
	assert.True(t, ok)
}

// Same heal, other side: re-removing a member whose row is already gone
// returns 404 but still attempts the idempotent revoke, clearing the
// stale g-line.
func TestRemoveMemberNotFoundStillRevokes(t *testing.T) {
	pool, ctx := newPool(t)
	ef, err := orgs.NewEnforcer(pool)
	require.NoError(t, err)
	svc := orgs.NewService(pool, nil, ef)
	u := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, u, "Acme")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `DELETE FROM memberships WHERE tenant_id=$1 AND user_id=$2`,
		pgid(org.TenantID), pgid(u))
	require.NoError(t, err)

	err = svc.RemoveMember(ctx, org.TenantID, u)
	assert.ErrorIs(t, err, errs.ErrNotFound)

	ok, err := ef.Enforce(u.String(), org.TenantID.String(), "/v1/orgs/x", "GET")
	require.NoError(t, err)
	assert.False(t, ok)
}
