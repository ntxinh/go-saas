package orgs_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/exodia/go-saas/internal/features/orgs"
	"github.com/exodia/go-saas/internal/shared/events"
	"github.com/exodia/go-saas/internal/shared/queue"
)

type sentMail struct{ to, subject, body string }

type fakeSender struct {
	calls []sentMail
	err   error
}

func (f *fakeSender) Send(_ context.Context, to, subject, body string) error {
	f.calls = append(f.calls, sentMail{to, subject, body})
	return f.err
}

func inviteTask(t *testing.T, p events.MemberInvited) *asynq.Task {
	t.Helper()
	raw, err := json.Marshal(p)
	require.NoError(t, err)
	// Enqueue injects a W3C traceparent key when ctx carries a span;
	// the payload must still unmarshal.
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	m["traceparent"] = "00-abc-def-01"
	raw, err = json.Marshal(m)
	require.NoError(t, err)
	return asynq.NewTask(queue.TaskEmailInvite, raw)
}

func TestHandleEmailInviteSendsRenderedMail(t *testing.T) {
	sender := &fakeSender{}
	p := events.MemberInvited{
		OrgID:       uuid.New(),
		OrgName:     "Acme",
		Email:       "new@acme.test",
		Token:       "tok-123",
		InviterName: "Boss",
	}
	h := orgs.HandleEmailInvite(sender, "https://app.test")

	require.NoError(t, h(context.Background(), inviteTask(t, p)))

	require.Len(t, sender.calls, 1)
	got := sender.calls[0]
	assert.Equal(t, "new@acme.test", got.to)
	assert.Contains(t, got.subject, "Acme")
	assert.Contains(t, got.body, "Acme")
	assert.Contains(t, got.body, "tok-123")
	assert.Contains(t, got.body, "https://app.test/invites/tok-123")
}

func TestHandleEmailInviteMalformedSkipsRetry(t *testing.T) {
	sender := &fakeSender{}
	h := orgs.HandleEmailInvite(sender, "https://app.test")

	err := h(context.Background(), asynq.NewTask(queue.TaskEmailInvite, []byte("not-json")))

	assert.ErrorIs(t, err, asynq.SkipRetry)
	assert.Empty(t, sender.calls)
}

func TestHandleEmailInviteSendErrorRetries(t *testing.T) {
	sender := &fakeSender{err: errors.New("smtp down")}
	h := orgs.HandleEmailInvite(sender, "https://app.test")

	err := h(context.Background(), inviteTask(t, events.MemberInvited{OrgName: "A", Email: "x@y.z", Token: "t"}))

	assert.Error(t, err)
	assert.NotErrorIs(t, err, asynq.SkipRetry)
}

func TestHandleInviteExpirySweepDeletesStale(t *testing.T) {
	svc, pool, ctx := newService(t)
	owner := addUser(t, pool, ctx)
	org, err := svc.Create(ctx, owner, "sweep-org")
	require.NoError(t, err)

	// One stale + one live invite, direct insert as owner (ExpireStale
	// itself bypasses RLS by design — verify both sides of the WHERE).
	_, err = pool.Exec(ctx,
		`INSERT INTO invites(tenant_id,email,role,token,expires_at) VALUES
		 ($1,'stale@x.test','member','stale-tok', now() - interval '1 day'),
		 ($1,'fresh@x.test','member','fresh-tok', now() + interval '7 day')`,
		org.TenantID)
	require.NoError(t, err)

	h := orgs.HandleInviteExpirySweep(svc)
	require.NoError(t, h(context.Background(), asynq.NewTask(queue.TaskInviteExpirySweep, nil)))

	var stale, fresh int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE token='stale-tok'),
		        count(*) FILTER (WHERE token='fresh-tok')
		 FROM invites`).Scan(&stale, &fresh))
	assert.Zero(t, stale)
	assert.Equal(t, 1, fresh)
}
