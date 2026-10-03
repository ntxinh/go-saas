package orgs_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/errs"
)

type capPub struct {
	topics   []string
	payloads []any
}

func (c *capPub) Publish(_ context.Context, topic string, payload any) error {
	c.topics = append(c.topics, topic)
	c.payloads = append(c.payloads, payload)
	return nil
}

func TestInviteAcceptFlow(t *testing.T) {
	_, pool, ctx := newService(t)
	pub := &capPub{}
	svc := orgs.NewService(pool, pub, nil)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	inv, err := svc.Invite(ctx, org.TenantID, "owner@x.y", "new@x.y", "member")
	require.NoError(t, err)
	assert.Equal(t, "new@x.y", inv.Email)
	assert.Equal(t, "member", inv.Role)
	assert.Len(t, inv.Token, 64) // 32B hex
	assert.True(t, inv.ExpiresAt.After(time.Now().Add(6*24*time.Hour)))

	invitee := addUser(ctx, t, pool)
	require.NoError(t, svc.Accept(ctx, inv.Token, invitee, "new@x.y"))

	role, ok := svc.IsMember(ctx, org.TenantID, invitee)
	require.True(t, ok)
	assert.Equal(t, "member", role)
	assert.Contains(t, pub.topics, "org.member_invited")
	assert.Contains(t, pub.topics, "org.member_joined")
}

func TestRevokeInviteRemovesInvite(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(ctx, t, svc, org.TenantID, "new@x.y")

	// owner can revoke; invite is gone afterwards (RBAC gates who may
	// call this at the route level — see rbac_e2e_test).
	require.NoError(t, svc.RevokeInvite(ctx, org.TenantID, inv.ID))
	err = svc.Accept(ctx, inv.Token, uuid.New(), "new@x.y")
	require.ErrorIs(t, err, errs.ErrNotFound)
}

func TestInviteRejectsBadRole(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	_, err = svc.Invite(ctx, org.TenantID, "o@x.y", "new@x.y", "owner")
	require.ErrorIs(t, err, errs.ErrValidation)
}

func invite(ctx context.Context, t *testing.T, svc *orgs.Service, orgID uuid.UUID, email string) orgs.Invite {
	t.Helper()
	inv, err := svc.Invite(ctx, orgID, "o@x.y", email, "member")
	require.NoError(t, err)
	return inv
}

func TestAcceptExpiredInvite(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(ctx, t, svc, org.TenantID, "new@x.y")

	_, err = pool.Exec(ctx, "UPDATE invites SET expires_at = now() - interval '1h' WHERE id = $1", pgid(inv.ID))
	require.NoError(t, err)

	err = svc.Accept(ctx, inv.Token, uuid.New(), "new@x.y")
	require.ErrorIs(t, err, errs.ErrValidation)
}

func TestAcceptEmailMismatch(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(ctx, t, svc, org.TenantID, "new@x.y")

	err = svc.Accept(ctx, inv.Token, uuid.New(), "other@x.y")
	require.ErrorIs(t, err, errs.ErrForbidden)
}

func TestAcceptReplay(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(ctx, t, pool)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(ctx, t, svc, org.TenantID, "new@x.y")
	invitee := addUser(ctx, t, pool)

	require.NoError(t, svc.Accept(ctx, inv.Token, invitee, "new@x.y"))
	err = svc.Accept(ctx, inv.Token, invitee, "new@x.y")
	require.ErrorIs(t, err, errs.ErrConflict)
}

func TestAcceptUnknownToken(t *testing.T) {
	svc, _, ctx := newService(t)
	err := svc.Accept(ctx, "deadbeef", uuid.New(), "a@b.c")
	require.ErrorIs(t, err, errs.ErrNotFound)
}
