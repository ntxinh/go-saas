# Design — go-saas

One-page summary of the approved spec:
[docs/superpowers/specs/2026-10-03-go-saas-backend-design.md](docs/superpowers/specs/2026-10-03-go-saas-backend-design.md)
(cited as *spec* below). That file is authoritative; this page is the map.

## Shape

Feature-sliced Go backend: `internal/features/*` slices (auth, users,
orgs, billing) may import `internal/shared/*` and themselves only.
Cross-feature facts move as `shared/events` payloads or `shared/queue`
asynq task types — never imports. `go-arch-lint` enforces the boundary
(spec §2).

## Request pipeline (outer → inner)

```
otelhttp → requestid → recover → CORS → ratelimit →
authn (JWKS) → idempotency (POST/PATCH) → tenant → rbac →
handler → service → WithTenantTx → sqlc
```

Errors exit as RFC 9457 `problem+json` via `errs.Write` (spec §4, §7).

## Tenancy

Shared Postgres, `tenant_id` on every tenant table, RLS policy
`tenant_id = current_setting('app.current_tenant')`. Services reach
tenant tables only through `database.WithTenantTx`, which issues
`SET LOCAL` as the tx's first statement — required because Supabase
PgBouncer pools per-transaction; a session-level `SET` would leak
tenants across checkouts. Queries still filter `WHERE tenant_id = $1`;
RLS is the safety net, not the filter (spec §5).

## Events vs asynq — the at-most-once rule

Watermill GoChannel events are **in-process fan-out only**: at-most-once.
Any side effect that must not be lost (email, cleanup) is enqueued as an
asynq task — directly by the service, or by an event subscriber; the
worker process runs handlers. Events also invalidate per-org Casbin
enforcer caches (spec §5).

## PII

AES-256-GCM at the repo boundary; 32-byte key from `PII_KEY` (64 hex
chars), random nonce prepended. Initially `users.phone` /
`users.display_name`; emails stay plaintext for lookup (spec §5).

## RBAC (Casbin)

RBAC-with-domains model: `g, user, role, org`; `p, role, org, obj, act`.
Policies live in Postgres (`casbin_rule`). Seeded: `owner` → all,
`admin` → members/invites, `member` → read. Enforcer loaded lazily
per-org (spec §5).

## Identity

Supabase Auth owns credentials; the API verifies RS256 JWTs via JWKS
(`JWKsURL()` = `SUPABASE_URL + /auth/v1/.well-known/jwks.json`) and
lazy-upserts `users` from claims on each request — no auth webhook
(spec §6).

## Migrations

Goose SQL files live at repo-root `migrations/` (two consumers: the
`goose` CLI via `make migrate`, and `database.Migrate` in-process).
`go:embed` cannot reach `../migrations` from `internal/`, so a thin
`migrations/migrations.go` package exposes `migrations.FS embed.FS`
at root; `Migrate` takes `pg_advisory_lock(727272)` on a pinned
session before `provider.Up` so racing replicas serialize.

## Toolchain

`mise.toml` `go` pin must match `go.mod`'s `go` directive (both 1.26;
goose v3.28/sqlc v1.31 force >= 1.26). Bump them together.
