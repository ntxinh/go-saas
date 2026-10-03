package orgs

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"strings"

	"github.com/hibiken/asynq"

	"github.com/ntxinh/go-saas/internal/shared/events"
	"github.com/ntxinh/go-saas/internal/shared/mail"
)

// HandleEmailInvite renders the invite email and sends it via sender.
// Malformed payloads SkipRetry; send errors bubble up so asynq retries.
// Unknown JSON keys (e.g. the injected traceparent) are ignored.
func HandleEmailInvite(sender mail.Sender, appURL string) asynq.HandlerFunc {
	tpl := template.Must(template.New("invite").Parse(`<!doctype html><html><body>
<p>{{.InviterName}} invited you to join <b>{{.OrgName}}</b> on go-saas.</p>
<p><a href="{{.URL}}">Accept the invitation</a></p>
</body></html>`))
	return func(ctx context.Context, t *asynq.Task) error {
		var p events.MemberInvited
		if err := json.Unmarshal(t.Payload(), &p); err != nil {
			return fmt.Errorf("orgs: %w: %w", err, asynq.SkipRetry)
		}
		var b strings.Builder
		if err := tpl.Execute(&b, struct {
			OrgName, InviterName, URL string
		}{p.OrgName, p.InviterName, appURL + "/invites/" + p.Token}); err != nil {
			return fmt.Errorf("orgs: invite email: %w", err)
		}
		subject := fmt.Sprintf("You've been invited to join %s", p.OrgName)
		if err := sender.Send(ctx, p.Email, subject, b.String()); err != nil {
			return fmt.Errorf("orgs: send invite: %w", err)
		}
		return nil
	}
}

// HandleInviteExpirySweep runs the cross-tenant stale-invite delete as
// the pool owner (RLS bypass intended for maintenance) and logs the
// count.
func HandleInviteExpirySweep(svc *Service) asynq.HandlerFunc {
	return func(ctx context.Context, _ *asynq.Task) error {
		n, err := svc.ExpireStale(ctx)
		if err != nil {
			return err
		}
		slog.Info("invite expiry sweep", "deleted", n)
		return nil
	}
}
