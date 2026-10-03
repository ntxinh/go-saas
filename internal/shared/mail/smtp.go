package mail

import (
	"context"
	"fmt"
	"net/smtp"
)

// smtpSender speaks plain unauthenticated SMTP (Mailpit in dev).
type smtpSender struct {
	addr, from string
}

// NewSMTP returns a Sender for a plain SMTP endpoint (no auth/TLS —
// dev Mailpit). Production mail goes through Resend.
func NewSMTP(addr, from string) Sender {
	return &smtpSender{addr: addr, from: from}
}

func (s *smtpSender) Send(_ context.Context, to, subject, html string) error {
	msg := "From: " + s.from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" + html
	if err := smtp.SendMail(s.addr, nil, s.from, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("mail: smtp: %w", err)
	}
	return nil
}
