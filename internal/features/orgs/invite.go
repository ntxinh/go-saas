package orgs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/exodia/go-saas/internal/features/orgs/sqlc"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/events"
)

const inviteTTL = 7 * 24 * time.Hour

// Invite is a pending org invitation.
type Invite struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func inviteFrom(i sqlc.Invite) Invite {
	return Invite{ID: i.ID.Bytes, Email: i.Email, Role: i.Role, Token: i.Token, ExpiresAt: i.ExpiresAt.Time}
}

// Invite creates a pending invite inside the org's tenant tx and
// publishes MemberInvited after commit. callerRole must be owner/admin
// (interim check; Task 7's casbin replaces it). Invited roles are
// limited to admin/member — owners are only created via org creation.
func (s *Service) Invite(ctx context.Context, orgID uuid.UUID, inviterEmail, callerRole, email, role string) (Invite, error) {
	if callerRole != "owner" && callerRole != "admin" {
		return Invite{}, errs.ErrForbidden
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if !strings.Contains(email, "@") {
		return Invite{}, errs.Validation(map[string]string{"email": "invalid"})
	}
	if role != "admin" && role != "member" {
		return Invite{}, errs.Validation(map[string]string{"role": "must be admin or member"})
	}
	var tok [32]byte
	if _, err := rand.Read(tok[:]); err != nil {
		return Invite{}, fmt.Errorf("orgs: invite token: %w", err)
	}
	token := hex.EncodeToString(tok[:])

	var inv Invite
	var orgName string
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx).q
		org, err := q.GetOrg(ctx, pgUUID(orgID))
		if err != nil {
			return err
		}
		orgName = org.Name
		row, err := q.CreateInvite(ctx, sqlc.CreateInviteParams{
			TenantID:  pgUUID(orgID),
			Email:     email,
			Role:      role,
			Token:     token,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(inviteTTL), Valid: true},
		})
		if err != nil {
			return err
		}
		inv = inviteFrom(row)
		return nil
	})
	if err != nil {
		return Invite{}, fmt.Errorf("orgs: invite: %w", err)
	}
	s.publish(ctx, events.TopicMemberInvited, events.MemberInvited{
		OrgID: orgID, OrgName: orgName, Email: inv.Email,
		Token: inv.Token, InviterName: inviterEmail,
	})
	return inv, nil
}

// Accept resolves an invite by token (owner-conn lookup — the token is
// the capability), then inserts the membership and marks the invite
// accepted inside the resolved org's tenant tx. Publishes MemberJoined.
func (s *Service) Accept(ctx context.Context, token string, userID uuid.UUID, email string) error {
	row, err := s.repo.q.InviteByToken(ctx, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("orgs: accept lookup: %w", err)
	}
	if row.AcceptedAt.Valid {
		return errs.ErrConflict
	}
	if time.Now().After(row.ExpiresAt.Time) {
		return errs.Validation(map[string]string{"invite": "expired"})
	}
	if !strings.EqualFold(row.Email, email) {
		return errs.ErrForbidden
	}
	orgID := row.TenantID.Bytes

	err = database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx).q
		if err := q.AddMember(ctx, sqlc.AddMemberParams{
			TenantID: pgUUID(orgID), UserID: pgUUID(userID), Role: row.Role,
		}); err != nil {
			return err
		}
		n, err := q.AcceptInvite(ctx, token)
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.ErrConflict
		}
		return nil
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return errs.ErrConflict
	case err != nil:
		return err
	}
	s.publish(ctx, events.TopicMemberJoined, events.MemberJoined{
		OrgID: orgID, UserID: userID, Role: row.Role,
	})
	return nil
}

// RevokeInvite deletes a pending invite inside the org's tenant tx.
func (s *Service) RevokeInvite(ctx context.Context, orgID, inviteID uuid.UUID) error {
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		n, err := s.repo.withTx(tx).q.RevokeInvite(ctx, sqlc.RevokeInviteParams{
			ID: pgUUID(inviteID), TenantID: pgUUID(orgID),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errs.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("orgs: revoke invite: %w", err)
	}
	return nil
}
