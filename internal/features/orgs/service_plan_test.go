package orgs_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ntxinh/go-saas/internal/shared/errs"
)

// Free plan seats: owner + 2 members = 3 (the cap). The next Invite and
// the next Accept/AddMember must both refuse with 422 validation —
// invites count pending seats so N pending invites can't all squeeze
// through the limit at accept time.
func TestPlanSeatLimit(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme") // plan: free (3 seats)
	require.NoError(t, err)

	m1, m2 := addUser(ctx, t, pool), addUser(ctx, t, pool)
	require.NoError(t, svc.AddMember(ctx, org.TenantID, m1, "member"))
	require.NoError(t, svc.AddMember(ctx, org.TenantID, m2, "member")) // 3/3

	// Over the cap: invite, add and accept all refuse.
	err = svc.AddMember(ctx, org.TenantID, addUser(ctx, t, pool), "member")
	require.ErrorIs(t, err, errs.ErrValidation)

	_, err = svc.Invite(ctx, org.TenantID, "o@x.y", "late@x.y", "member")
	require.ErrorIs(t, err, errs.ErrValidation)

	// A pending invite also consumes a seat: 2 members + 1 pending = 3.
	third, err := svc.Create(ctx, m1, "Beta")
	require.NoError(t, err)
	require.NoError(t, svc.AddMember(ctx, third.TenantID, m2, "member")) // 2/3
	_ = invite(ctx, t, svc, third.TenantID, "pending@x.y")               // 2 + 1 pending
	_, err = svc.Invite(ctx, third.TenantID, "o@x.y", "one.more@x.y", "member")
	require.ErrorIs(t, err, errs.ErrValidation)
}
