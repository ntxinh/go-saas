package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const resendURL = "https://api.resend.com/emails"

type resend struct {
	key, from, baseURL string
	client             *http.Client
}

// NewResend returns a Sender that POSTs to the Resend API.
func NewResend(key, from string) Sender {
	return &resend{key: key, from: from, baseURL: resendURL, client: &http.Client{Timeout: 10 * time.Second}}
}

func (s *resend) Send(ctx context.Context, to, subject, html string) error {
	body, err := json.Marshal(map[string]any{
		"from":    s.from,
		"to":      []string{to},
		"subject": subject,
		"html":    html,
	})
	if err != nil {
		return fmt.Errorf("mail: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("mail: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.key)
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("mail: send: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("mail: resend: %s", res.Status)
	}
	return nil
}
