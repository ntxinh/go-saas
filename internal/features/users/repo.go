package users

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	usersqlc "github.com/exodia/go-saas/internal/features/users/sqlc"
)

// Repo wraps the sqlc queries for the users table (not tenant-scoped).
type Repo struct {
	q *usersqlc.Queries
}

func NewRepo(db usersqlc.DBTX) *Repo { return &Repo{q: usersqlc.New(db)} }

func pgUUID(id uuid.UUID) pgtype.UUID { return pgtype.UUID{Bytes: id, Valid: true} }

func (r *Repo) Sync(ctx context.Context, id uuid.UUID, email string) error {
	return r.q.SyncUser(ctx, usersqlc.SyncUserParams{ID: pgUUID(id), Email: email})
}

func (r *Repo) Get(ctx context.Context, id uuid.UUID) (usersqlc.GetUserRow, error) {
	return r.q.GetUser(ctx, pgUUID(id))
}
