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
	svc, pool, ctx := newService(t)
	pub := &capPub{}
	svc = orgs.NewService(pool, pub)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	inv, err := svc.Invite(ctx, org.TenantID, "owner@x.y", "owner", "new@x.y", "member")
	require.NoError(t, err)
	assert.Equal(t, "new@x.y", inv.Email)
	assert.Equal(t, "member", inv.Role)
	assert.Len(t, inv.Token, 64) // 32B hex
	assert.True(t, inv.ExpiresAt.After(time.Now().Add(6*24*time.Hour)))

	invitee := addUser(t, pool, ctx)
	require.NoError(t, svc.Accept(ctx, inv.Token, invitee, "new@x.y"))

	role, ok := svc.IsMember(ctx, org.TenantID, invitee)
	require.True(t, ok)
	assert.Equal(t, "member", role)

	assert.Contains(t, pub.topics, "org.member_invited")
	assert.Contains(t, pub.topics, "org.member_joined")
}

func TestInviteForbiddenForMember(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	_, err = svc.Invite(ctx, org.TenantID, "m@x.y", "member", "new@x.y", "member")
	assert.ErrorIs(t, err, errs.ErrForbidden)
}

func TestInviteRejectsBadRole(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)

	_, err = svc.Invite(ctx, org.TenantID, "o@x.y", "owner", "new@x.y", "owner")
	assert.ErrorIs(t, err, errs.ErrValidation)
}

func invite(t *testing.T, svc *orgs.Service, ctx context.Context, orgID uuid.UUID, email string) orgs.Invite {
	t.Helper()
	inv, err := svc.Invite(ctx, orgID, "o@x.y", "owner", email, "member")
	require.NoError(t, err)
	return inv
}

func TestAcceptExpiredInvite(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(t, svc, ctx, org.TenantID, "new@x.y")

	_, err = pool.Exec(ctx, "UPDATE invites SET expires_at = now() - interval '1h' WHERE id = $1", pgid(inv.ID))
	require.NoError(t, err)

	err = svc.Accept(ctx, inv.Token, uuid.New(), "new@x.y")
	assert.ErrorIs(t, err, errs.ErrValidation)
}

func TestAcceptEmailMismatch(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(t, svc, ctx, org.TenantID, "new@x.y")

	err = svc.Accept(ctx, inv.Token, uuid.New(), "other@x.y")
	assert.ErrorIs(t, err, errs.ErrForbidden)
}

func TestAcceptReplay(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "Acme")
	require.NoError(t, err)
	inv := invite(t, svc, ctx, org.TenantID, "new@x.y")
	invitee := addUser(t, pool, ctx)

	require.NoError(t, svc.Accept(ctx, inv.Token, invitee, "new@x.y"))
	err = svc.Accept(ctx, inv.Token, invitee, "new@x.y")
	assert.ErrorIs(t, err, errs.ErrConflict)
}

func TestAcceptUnknownToken(t *testing.T) {
	svc, _, ctx := newService(t)
	err := svc.Accept(ctx, "deadbeef", uuid.New(), "a@b.c")
	assert.ErrorIs(t, err, errs.ErrNotFound)
}
