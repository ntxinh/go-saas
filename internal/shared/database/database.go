// Package database owns the pgx pool, goose migrations and the
// transaction helpers every tenant-scoped query must go through.
package database

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool parses url into a pgx pool (MaxConns 10). The otelpgx tracer
// resolves the global provider at use time — free when OTel is noop.
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("database: parse url: %w", err)
	}
	cfg.MaxConns = 10
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping: %w", err)
	}
	return pool, nil
}

// TxFn runs inside a transaction; returning an error rolls back.
type TxFn func(tx pgx.Tx) error

// WithTx runs fn in a transaction: commit on nil, rollback on error
// or panic (panic re-throws after rollback via the deferred call).
func WithTx(ctx context.Context, pool *pgxpool.Pool, fn TxFn) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin: %w", err)
	}
	defer tx.Rollback(ctx) // no-op after Commit; rolls back on panic
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// WithTenantTx runs fn as app_user with app.current_tenant set for
// RLS. Both SETs are LOCAL/first-statement so pooled connections
// (PgBouncer transaction mode) never leak tenant state.
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, fn TxFn) error {
	return WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SET LOCAL ROLE app_user`); err != nil {
			return fmt.Errorf("database: set role: %w", err)
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('app.current_tenant', $1, true)`, tenantID); err != nil {
			return fmt.Errorf("database: set tenant: %w", err)
		}
		return fn(tx)
	})
}
