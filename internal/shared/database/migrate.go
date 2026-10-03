package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/exodia/go-saas/migrations"
)

// migrateLockKey serializes concurrent migrators (multiple replicas
// booting at once) via a Postgres session-level advisory lock.
const migrateLockKey = 727272

// Migrate applies all embedded goose migrations under an advisory
// lock. Safe to call from every process at startup.
func Migrate(ctx context.Context, url string) error {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return fmt.Errorf("database: parse url: %w", err)
	}
	db := stdlib.OpenDB(*cfg)
	defer func() { _ = db.Close() }()

	// Advisory lock is session-scoped: pin one conn for lock+unlock.
	lock, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("database: lock conn: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if _, err := lock.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, migrateLockKey); err != nil {
		return fmt.Errorf("database: advisory lock: %w", err)
	}
	defer func() { _, _ = lock.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, migrateLockKey) }()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		return fmt.Errorf("database: goose provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("database: migrate up: %w", err)
	}
	return nil
}
