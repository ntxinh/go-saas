// Package orgs owns orgs + memberships: tenant resolution for the
// middleware (as pool owner, bypassing RLS) and tenant-scoped CRUD that
// always runs inside database.WithTenantTx.
package orgs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/exodia/go-saas/internal/features/orgs/sqlc"
	"github.com/exodia/go-saas/internal/shared/database"
	"github.com/exodia/go-saas/internal/shared/errs"
	"github.com/exodia/go-saas/internal/shared/events"
	"github.com/exodia/go-saas/internal/shared/middleware"
	"github.com/exodia/go-saas/internal/shared/plans"
)

// Org is a tenant.
type Org struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

// OrgRef is a user's membership in an org — the shape /v1/me and
// GET /v1/orgs return.
type OrgRef struct {
	TenantID uuid.UUID `json:"tenant_id"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
}

// Member is a membership row.
type Member struct {
	UserID    uuid.UUID `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// Publisher is the optional event seam — Task 5 wires the real events
// publisher; nil means "don't publish".
type Publisher interface {
	Publish(ctx context.Context, topic string, payload any) error
}

// Authorizer is the optional casbin seam — Task 7 wires orgs.Enforcer;
// nil means "don't sync g-lines". Membership writes grant/revoke so the
// enforcer's grouping policies track the members table.
type Authorizer interface {
	Grant(ctx context.Context, orgID, userID, role string) error
	Revoke(ctx context.Context, orgID, userID string) error
}

// Service implements org use-cases. Tenant-scoped methods take the orgID
// explicitly (the Tenant middleware has already verified membership);
// they still run under WithTenantTx so RLS is the backstop.
type Service struct {
	pool  *pgxpool.Pool
	repo  *Repo
	pub   Publisher
	authz Authorizer
}

// NewService builds the orgs service. pub and authz may be nil.
func NewService(pool *pgxpool.Pool, pub Publisher, authz Authorizer) *Service {
	return &Service{pool: pool, repo: NewRepo(pool), pub: pub, authz: authz}
}

func orgFrom(o sqlc.Org) Org {
	return Org{TenantID: o.TenantID.Bytes, Name: o.Name, Plan: o.Plan, CreatedAt: o.CreatedAt.Time}
}

func validRole(role string) bool {
	return role == "owner" || role == "admin" || role == "member"
}

// seatCheck enforces the org's plan seat cap. It runs inside the tenant
// tx (counts are consistent with the pending write) and returns 422
// validation when plan + count would exceed plans.Can. Callers pass the
// seats to check: members+1 for direct adds, members+pending invites
// when creating an invite (pending invites hold seats).
func (s *Service) seatCheck(ctx context.Context, q *sqlc.Queries, orgID uuid.UUID, want int64) error {
	org, err := q.GetOrg(ctx, pgUUID(orgID))
	if err != nil {
		return err
	}
	if !plans.Can(org.Plan, "members", int(want)) {
		return errs.Validation(map[string]string{
			"members": fmt.Sprintf("plan %q seat limit reached", org.Plan),
		})
	}
	return nil
}

// Create inserts the org and the caller's owner membership in one tx.
// It uses WithTx (no tenant exists yet) with explicit tenant_ids.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, errs.Validation(map[string]string{"name": "required"})
	}
	var org Org
	err := database.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx)
		row, err := q.q.CreateOrg(ctx, name)
		if err != nil {
			return fmt.Errorf("orgs: create: %w", err)
		}
		org = orgFrom(row)
		return q.q.AddMember(ctx, sqlc.AddMemberParams{
			TenantID: row.TenantID,
			UserID:   pgUUID(userID),
			Role:     "owner",
		})
	})
	if err != nil {
		return Org{}, err
	}
	s.publish(ctx, "org.created", org)
	return org, s.grant(ctx, org.TenantID, userID, "owner")
}

// IsMember resolves membership as the pool owner — deliberately outside
// RLS, this is the resolution step the Tenant middleware calls.
func (s *Service) IsMember(ctx context.Context, orgID, userID uuid.UUID) (string, bool) {
	role, err := s.repo.q.MemberRole(ctx, sqlc.MemberRoleParams{
		TenantID: pgUUID(orgID), UserID: pgUUID(userID),
	})
	if err != nil {
		return "", false
	}
	return role, true
}

// OrgsOf lists the user's memberships as the pool owner (cross-tenant
// by design — the user is asking for their own list).
func (s *Service) OrgsOf(ctx context.Context, userID uuid.UUID) ([]OrgRef, error) {
	rows, err := s.repo.q.ListUserOrgs(ctx, pgUUID(userID))
	if err != nil {
		return nil, fmt.Errorf("orgs: list user orgs: %w", err)
	}
	out := make([]OrgRef, len(rows))
	for i, r := range rows {
		out[i] = OrgRef{TenantID: r.TenantID.Bytes, Name: r.Name, Role: r.Role}
	}
	return out, nil
}

// Get returns the org, scoped to its own tenant tx.
func (s *Service) Get(ctx context.Context, orgID uuid.UUID) (Org, error) {
	var org Org
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		row, err := s.repo.withTx(tx).q.GetOrg(ctx, pgUUID(orgID))
		if err != nil {
			return err
		}
		org = orgFrom(row)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, errs.ErrNotFound
	}
	if err != nil {
		return Org{}, fmt.Errorf("orgs: get: %w", err)
	}
	return org, nil
}

// Update renames the org inside its tenant tx.
func (s *Service) Update(ctx context.Context, orgID uuid.UUID, name string) (Org, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Org{}, errs.Validation(map[string]string{"name": "required"})
	}
	var org Org
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		row, err := s.repo.withTx(tx).q.UpdateOrg(ctx, sqlc.UpdateOrgParams{
			TenantID: pgUUID(orgID), Name: name,
		})
		if err != nil {
			return err
		}
		org = orgFrom(row)
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Org{}, errs.ErrNotFound
	}
	if err != nil {
		return Org{}, fmt.Errorf("orgs: update: %w", err)
	}
	return org, nil
}

// Members lists the org's memberships inside its tenant tx.
func (s *Service) Members(ctx context.Context, orgID uuid.UUID) ([]Member, error) {
	var out []Member
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		rows, err := s.repo.withTx(tx).q.ListMembers(ctx, pgUUID(orgID))
		if err != nil {
			return err
		}
		out = make([]Member, len(rows))
		for i, r := range rows {
			out[i] = Member{UserID: r.UserID.Bytes, Role: r.Role, CreatedAt: r.CreatedAt.Time}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("orgs: members: %w", err)
	}
	return out, nil
}

// AddMember inserts a membership inside the org's tenant tx. Duplicates
// are 409; unknown users 422.
func (s *Service) AddMember(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	if !validRole(role) {
		return errs.Validation(map[string]string{"role": "must be owner, admin or member"})
	}
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx).q
		seats, err := q.SeatCounts(ctx, pgUUID(orgID))
		if err != nil {
			return err
		}
		if err := s.seatCheck(ctx, q, orgID, seats.Members+1); err != nil {
			return err
		}
		return q.AddMember(ctx, sqlc.AddMemberParams{
			TenantID: pgUUID(orgID), UserID: pgUUID(userID), Role: role,
		})
	})
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "23505":
		return errs.ErrConflict
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		return errs.Validation(map[string]string{"user_id": "unknown user"})
	case err != nil:
		return fmt.Errorf("orgs: add member: %w", err)
	}
	s.publish(ctx, "org.member_added", Member{UserID: userID, Role: role})
	return s.grant(ctx, orgID, userID, role)
}

// RemoveMember deletes a membership. Refuses to remove the org's last
// owner (counted inside the same tenant tx).
func (s *Service) RemoveMember(ctx context.Context, orgID, userID uuid.UUID) error {
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx).q
		if err := s.guardLastOwner(ctx, q, orgID, userID, ""); err != nil {
			return err
		}
		n, err := q.RemoveMember(ctx, sqlc.RemoveMemberParams{
			TenantID: pgUUID(orgID), UserID: pgUUID(userID),
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
		if errors.Is(err, errs.ErrNotFound) {
			// Row already gone, but a prior RemoveMember may have committed
			// the delete then failed its revoke — attempt the heal anyway
			// (idempotent). Caller still sees 404.
			_ = s.revoke(ctx, orgID, userID)
		}
		return err
	}
	s.publish(ctx, events.TopicMemberRemoved, events.MemberRemoved{OrgID: orgID, UserID: userID})
	return s.revoke(ctx, orgID, userID)
}

// ChangeRole updates a member's role. Refuses to demote the last owner.
func (s *Service) ChangeRole(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	if !validRole(role) {
		return errs.Validation(map[string]string{"role": "must be owner, admin or member"})
	}
	err := database.WithTenantTx(ctx, s.pool, orgID, func(tx pgx.Tx) error {
		q := s.repo.withTx(tx).q
		if err := s.guardLastOwner(ctx, q, orgID, userID, role); err != nil {
			return err
		}
		n, err := q.ChangeRole(ctx, sqlc.ChangeRoleParams{
			TenantID: pgUUID(orgID), UserID: pgUUID(userID), Role: role,
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
		return err
	}
	s.publish(ctx, events.TopicRoleChanged, events.RoleChanged{OrgID: orgID, UserID: userID, Role: role})
	if err := s.revoke(ctx, orgID, userID); err != nil {
		return err
	}
	return s.grant(ctx, orgID, userID, role)
}

// guardLastOwner returns ErrValidation when the target is an owner, the
// change removes or demotes them, and no other owner remains. newRole
// "" means removal.
func (s *Service) guardLastOwner(ctx context.Context, q *sqlc.Queries, orgID, userID uuid.UUID, newRole string) error {
	if newRole == "owner" {
		return nil
	}
	cur, err := q.MemberRole(ctx, sqlc.MemberRoleParams{
		TenantID: pgUUID(orgID), UserID: pgUUID(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // delete/update path reports not-found itself
	}
	if err != nil {
		return err
	}
	if cur != "owner" {
		return nil
	}
	owners, err := q.CountOwners(ctx, pgUUID(orgID))
	if err != nil {
		return err
	}
	if owners <= 1 {
		return errs.Validation(map[string]string{"role": "cannot remove the last owner"})
	}
	return nil
}

// publish emits an event when a publisher is wired; failures are logged
// and swallowed — events are best-effort (at-most-once GoChannel bus).
func (s *Service) publish(ctx context.Context, topic string, payload any) {
	if s.pub == nil {
		return
	}
	if err := s.pub.Publish(ctx, topic, payload); err != nil {
		middleware.Logger(ctx).Warn("event publish failed", "topic", topic, "err", err)
	}
}

// grant/revoke sync casbin g-lines after a committed membership write.
// Failures are logged AND returned: casbin_rule is the authz ledger, so a
// swallowed desync could leave a stale grant (e.g. a demoted owner keeps
// their role:owner g-line). The op's DB write already committed — the
// error tells the caller "authz desynced"; retrying the op heals it
// (grants/revokes are idempotent). Unlike publish, these propagate.
func (s *Service) grant(ctx context.Context, orgID, userID uuid.UUID, role string) error {
	if s.authz == nil {
		return nil
	}
	if err := s.authz.Grant(ctx, orgID.String(), userID.String(), role); err != nil {
		middleware.Logger(ctx).Warn("authz grant failed", "org", orgID, "user", userID, "err", err)
		return fmt.Errorf("orgs: authz grant: %w", err)
	}
	return nil
}

func (s *Service) revoke(ctx context.Context, orgID, userID uuid.UUID) error {
	if s.authz == nil {
		return nil
	}
	if err := s.authz.Revoke(ctx, orgID.String(), userID.String()); err != nil {
		middleware.Logger(ctx).Warn("authz revoke failed", "org", orgID, "user", userID, "err", err)
		return fmt.Errorf("orgs: authz revoke: %w", err)
	}
	return nil
}
