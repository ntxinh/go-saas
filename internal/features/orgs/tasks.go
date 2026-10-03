package orgs

import (
	"context"
	"fmt"
)

// ExpireStale hard-deletes unaccepted invites past expiry. Runs as the
// pool owner (cross-tenant maintenance, invoked by the
// TaskInviteExpirySweep worker); returns the number deleted.
func (s *Service) ExpireStale(ctx context.Context) (int, error) {
	n, err := s.repo.q.ExpireStaleInvites(ctx)
	if err != nil {
		return 0, fmt.Errorf("orgs: expire invites: %w", err)
	}
	return int(n), nil
}
