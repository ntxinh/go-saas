// Package mail sends transactional email. v1 has two senders: Resend
// (HTTP API) and plain SMTP (Mailpit in dev).
package mail

import "context"

// Sender sends one HTML email.
type Sender interface {
	Send(ctx context.Context, to, subject, html string) error
}
