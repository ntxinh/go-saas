// Command seed populates dev databases with fake users, orgs,
// memberships and invites. It connects as the pool owner — seeds are
// maintenance, so RLS bypass is intended.
//
//	go run ./cmd/seed -orgs 5 -users 20          # users via Supabase Admin API
//	go run ./cmd/seed -orgs 5 -users 20 -local   # direct inserts, no Supabase
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type options struct {
	orgs  int
	users int
	local bool
}

func parseFlags(fs *flag.FlagSet, args []string) (options, error) {
	var o options
	fs.IntVar(&o.orgs, "orgs", 5, "orgs to create")
	fs.IntVar(&o.users, "users", 10, "users to create")
	fs.BoolVar(&o.local, "local", false, "insert users directly (no Supabase calls)")
	return o, fs.Parse(args)
}

func main() {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	o, err := parseFlags(fs, os.Args[1:])
	if err != nil {
		slog.Error("flags", "err", err)
		os.Exit(2)
	}
	if err := run(context.Background(), o); err != nil {
		slog.Error("seed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("seed: connect: %w", err)
	}
	defer pool.Close()

	seeder := seedUserLocal(pool)
	if !o.local {
		key := os.Getenv("SUPABASE_SERVICE_KEY")
		base := os.Getenv("SUPABASE_URL")
		if key == "" || base == "" {
			return errors.New("SUPABASE_URL and SUPABASE_SERVICE_KEY are required without -local")
		}
		seeder = seedUserSupabase(pool, base, key)
	}

	ids, err := seedUsers(ctx, o.users, pool, seeder)
	if err != nil {
		return err
	}
	return seedOrgs(ctx, o.orgs, ids, pool)
}

// seedUser inserts one user row; local mode generates the uuid, Supabase
// mode returns the auth user's id. It reports whether the row was new.
type seedUser func(ctx context.Context, email string) (uuid.UUID, bool, error)

// seedUsers creates users idempotently: an existing email is skipped.
func seedUsers(ctx context.Context, n int, pool *pgxpool.Pool, seed seedUser) ([]uuid.UUID, error) {
	fk := gofakeit.New(0)
	ids := make([]uuid.UUID, 0, n)
	for range n {
		email := fmt.Sprintf("%s@%s", fk.Username(), fk.DomainName())
		var existing uuid.UUID
		err := pool.QueryRow(ctx, "SELECT id FROM users WHERE email = $1", email).Scan(&existing)
		switch {
		case err == nil:
			slog.Info("user exists, skipping", "email", email)
			ids = append(ids, existing)
			continue
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, fmt.Errorf("seed: user lookup %s: %w", email, err)
		}
		id, created, err := seed(ctx, email)
		if err != nil {
			return nil, fmt.Errorf("seed: user %s: %w", email, err)
		}
		slog.Info("user seeded", "email", email, "id", id, "created", created)
		ids = append(ids, id)
	}
	return ids, nil
}

func seedUserLocal(pool *pgxpool.Pool) seedUser {
	return func(ctx context.Context, email string) (uuid.UUID, bool, error) {
		id := uuid.New()
		_, err := pool.Exec(ctx, "INSERT INTO users(id, email) VALUES($1, $2)", id, email)
		return id, err == nil, err
	}
}

// seedUserSupabase creates the auth user via the Supabase Admin API and
// mirrors the row into our users table (the UpsertUser middleware would
// do this on first sign-in; seeds do it upfront so memberships can FK).
func seedUserSupabase(pool *pgxpool.Pool, base, key string) seedUser {
	return func(ctx context.Context, email string) (uuid.UUID, bool, error) {
		body, _ := json.Marshal(map[string]any{
			"email": email, "password": "Password1!", "email_confirm": true,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			base+"/auth/v1/admin/users", bytes.NewReader(body))
		if err != nil {
			return uuid.Nil, false, err
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("apikey", key)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return uuid.Nil, false, err
		}
		defer func() { _ = resp.Body.Close() }()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return uuid.Nil, false, err
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			return uuid.Nil, false, fmt.Errorf("admin api %d: %s", resp.StatusCode, raw)
		}
		var out struct {
			ID uuid.UUID `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return uuid.Nil, false, fmt.Errorf("admin api decode: %w", err)
		}
		_, err = pool.Exec(ctx, "INSERT INTO users(id, email) VALUES($1, $2)", out.ID, email)
		return out.ID, err == nil, err
	}
}

// seedOrgs creates orgs with an owner, one extra member and a pending
// invite. Org names are matched for idempotent reruns.
func seedOrgs(ctx context.Context, n int, users []uuid.UUID, pool *pgxpool.Pool) error {
	if len(users) == 0 {
		return errors.New("no users to own orgs")
	}
	fk := gofakeit.New(0)
	for i := range n {
		owner := users[i%len(users)]
		member := users[(i+1)%len(users)]
		name := fk.Company()

		var orgID uuid.UUID
		err := pool.QueryRow(ctx, "SELECT tenant_id FROM orgs WHERE name = $1", name).Scan(&orgID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = pool.QueryRow(ctx, "INSERT INTO orgs(name) VALUES($1) RETURNING tenant_id", name).Scan(&orgID)
			if err == nil {
				slog.Info("org seeded", "name", name, "tenant", orgID)
			}
		case err == nil:
			slog.Info("org exists, reusing", "name", name)
		}
		if err != nil {
			return fmt.Errorf("seed: org %s: %w", name, err)
		}

		for _, m := range []struct {
			id   uuid.UUID
			role string
		}{{owner, "owner"}, {member, "member"}} {
			_, err := pool.Exec(ctx,
				"INSERT INTO memberships(tenant_id, user_id, role) VALUES($1, $2, $3) ON CONFLICT DO NOTHING",
				orgID, m.id, m.role)
			if err != nil {
				return fmt.Errorf("seed: membership %s: %w", name, err)
			}
		}

		// One pending invite per org, keyed off the org so reruns reuse.
		inviteEmail := fmt.Sprintf("invite+%s@seed.local", orgID.String()[:8])
		_, err = pool.Exec(ctx,
			`INSERT INTO invites(tenant_id, email, role, token, expires_at)
			 VALUES($1, $2, 'member', $3, now() + interval '7 days')
			 ON CONFLICT (token) DO NOTHING`,
			orgID, inviteEmail, "seed-"+orgID.String())
		if err != nil {
			return fmt.Errorf("seed: invite %s: %w", name, err)
		}
	}
	return nil
}
