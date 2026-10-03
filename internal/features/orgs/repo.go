package orgs

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/exodia/go-saas/internal/features/orgs/sqlc"
)

// Repo wraps the sqlc queries for orgs + memberships. It is bound to a
// DBTX at construction: the pool for owner-level queries (membership
// resolution, per-user org lists) or a tenant tx for scoped queries.
type Repo struct {
	q *sqlc.Queries
}

// NewRepo binds the orgs queries to a pool or tx.
func NewRepo(db sqlc.DBTX) *Repo { return &Repo{q: sqlc.New(db)} }

func (r *Repo) withTx(tx pgx.Tx) *Repo { return &Repo{q: r.q.WithTx(tx)} }

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }
