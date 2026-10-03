# go-saas — Backend Design Spec

**Status:** approved design, pre-implementation
**Product:** B2B team-workspace SaaS (orgs, members, invites, roles; plan tiers stubbed)
**Team:** one human + AI agents
**Mode:** greenfield Go backend, approach "B" — full chosen stack, pragmatic seams

## 1. Goals & Non-Goals

### Goals
- Multi-tenant B2B workspace API: organizations, memberships, invitations, RBAC roles.
- Supabase Auth for identity; service owns org/role/membership domain model.
- The four v1 infrastructure pieces the owner selected: Casbin RBAC, asynq background jobs, Watermill domain events, OpenTelemetry tracing to Grafana Cloud.
- Agent-friendly codebase: flat wiring, strict feature boundaries enforced in CI.
- Free-tier services only: Supabase (Postgres + PgBouncer), Upstash (Redis), Grafana Cloud (OTLP), Resend (mail), Cloudflare R2 (storage, when needed), GitHub Actions (CI).
- Podman locally instead of Docker.

### Non-goals for v1
- Real payments — `billing` slice holds a `plan` field + entitlement check only; no Stripe.
- `pkg/` — nothing is public yet.
- Feature flags (PostHog) — nothing to gate until a paid tier exists.
- Self-hosted CI runners — revisit if GitHub-hosted minutes become a real cost.
- Redis Streams as event transport — single cross-process mechanism is asynq.
- `uber/fx` — manual wiring instead (below).
- A specific domain feature (projects/docs/issues) — the workspace foundation only.

## 2. Architecture

Feature-Sliced Design adapted as vertical slices: group by domain feature, never by technical layer. Boundary rule: **a feature may import only `internal/shared/*` and itself — never another feature's internals.** Cross-feature facts travel as event payloads in `shared/events` or asynq task types in `shared/queue`. Enforced by `go-arch-lint` in CI and by convention (feature internals are not exported through `feature.go`).

### Layout

```
├── cmd/
│   ├── api/main.go        # config → otel/slog → wire → serve
│   ├── worker/main.go     # asynq server, same module, own entrypoint
│   └── seed/main.go       # gofakeit seeds via Supabase service key + API
├── internal/
│   ├── app/wire.go        # manual DI: flat file, every constructor in order
│   ├── shared/
│   │   ├── config/        # caarlos0/env, fail-fast validation
│   │   ├── database/      # pgxpool, WithTx, WithTenantTx (SET LOCAL), sqlc glue
│   │   ├── events/        # watermill router (GoChannel) + event payload structs
│   │   ├── queue/         # asynq client + typed enqueue helpers
│   │   ├── middleware/    # requestid, otel, authn, tenant, rbac, ratelimit, idempotency
│   │   ├── server/        # chi router + graceful shutdown (errgroup)
│   │   ├── security/      # AES-256-GCM encrypt/decrypt for PII columns
│   │   └── errs/          # RFC 9457 problem+json, sentinel → status mapping
│   └── features/
│       ├── auth/          # Supabase JWT verify (JWKS via MicahParks/keyfunc), GET /v1/me
│       ├── users/         # profile, lazy upsert from JWT claims
│       ├── orgs/          # tenants: CRUD, members, invites, role changes
│       └── billing/       # plan field + Can(plan, feature) entitlement check
├── migrations/            # goose *.sql
├── deploy/
│   ├── Containerfile      # multi-stage → scratch
│   └── compose.yml        # local deps (Mailpit, postgres, redis) for podman-compose
└── .github/workflows/ci.yml
```

Each feature contains: `feature.go` (constructor + `RegisterRoutes`), `handler.go`, `service.go`, `repo.go` (interface), `sqlc/` (generated queries), `events.go` (published), `tasks.go` (enqueued). `main.go` ≤ ~40 lines; `wire.go` is the entire object graph — flat, compile-time-checked.

## 3. Stack (chosen)

| Concern | Choice |
|---|---|
| Router | `go-chi/chi` v5 |
| Validation | `go-playground/validator/v10` |
| Config | `caarlos0/env` + `.env` |
| DB | Supabase Postgres via `pgx/v5` pool + PgBouncer (transaction mode) |
| Queries | `sqlc` (sqlc.yaml: pgx/v5, `sql_package: pgx/v5`) |
| Migrations | `pressly/goose` |
| Cache/queue/idempotency/ratelimit | Upstash Redis via `go-redis` v9 (key-prefixed, one DB) |
| Jobs | `hibiken/asynq` (+ asynqmon optional) |
| Events | `ThreeDotsLabs/watermill` **GoChannel (in-process only)** |
| Auth | Supabase Auth; `golang-jwt/v5` + `MicahParks/keyfunc` JWKS |
| RBAC | `casbin/casbin`, RBAC-with-domains model, policies in Postgres |
| Logging | `log/slog` JSON; `trace_id` injected from otel ctx |
| Tracing | `otel` + `otelhttp` + `otelpgx`, OTLP → Grafana Cloud (no-op exporter if unconfigured) |
| Idempotency | custom chi middleware on Upstash |
| Mail | Resend API in prod, Mailpit locally |
| Seeds | `brianvoe/gofakeit` |
| Tests | `testify`, `testcontainers-go` on **Podman**, `httptest` for E2E |
| Lint/fmt | `golangci-lint` (strict), `gofumpt` |
| CI | GitHub-hosted runners, `go-arch-lint` boundary check |

## 4. Request Pipeline

Outer → inner (chi middlewares):

1. `otelhttp` — span + trace context
2. `requestid` — `slog` logger with `request_id`, `trace_id` into ctx
3. `recover` → 500 problem
4. CORS
5. `ratelimit` — `go-redis/redis_rate`, key `rl:{ip}` public, `rl:{user}` authed
6. `authn` — JWKS verify (Supabase RS256), puts `user_id`; lazy user upsert (§6)
7. `idempotency` — POST/PATCH only (§5)
8. `tenant` — for `/{orgID}/...` routes: membership check (one indexed query), `tenantID` into ctx
9. `rbac` — Casbin `Enforce(user, org, obj, act)`
10. Handler → `service` → `WithTenantTx` → sqlc → `handler` writes `problem+json` on error

## 5. Key Mechanisms

### Tenancy
- Shared DB; every tenant table has `tenant_id uuid` + RLS policy `tenant_id = current_setting('app.current_tenant')::uuid`.
- Services run queries only via `database.WithTenantTx(ctx, func(tx *sqlc.Queries) error)` which issues `SET LOCAL app.current_tenant = $1` as the transaction's first statement.
- `SET LOCAL` inside a real tx is required — Supabase PgBouncer runs transaction-mode pooling; session-level `SET` would leak the tenant across checkouts.
- Non-tenant paths (login, org create, internal ops) use `WithTx` without tenant context.
- Belt-and-suspenders: queries also filter `WHERE tenant_id = $1` in SQL — RLS is the safety net, not the primary filter.

### Idempotency
- Client sends `Idempotency-Key: <uuid>` on POST/PATCH. Redis key: `idem:{user}:{method}:{path}:{key}`, TTL 24h.
- `SET NX` → in-flight → `409`; done → replay cached status+body; free → run handler, capture response, store.
- Stored response captured via `ResponseWriter` wrapper.

### Events & async
- Watermill GoChannel router lives in `internal/shared/events` and is started in the API process.
- Publishing service publishes payload structs from `shared/events` (e.g. `orgs.MemberInvited`, `orgs.MemberRemoved`, `users.Registered`).
- Subscribers needing side-effects enqueue typed asynq tasks (`email:invite`, `org:invite-expiry`); the **worker** process runs handlers (Resend send, scheduled cleanup).
- Contract: in-process events are at-most-once. If a side-effect must not be lost, the service enqueues the asynq task directly — events are for fan-out convenience, not durability.
- Casbin enforcer is loaded lazily per-org and invalidated by `orgs.RoleChanged` / `orgs.MemberRemoved` events in-process.

### PII
- `security.Encrypt/Decrypt` — AES-256-GCM, 32-byte key from env (`PII_KEY`), random nonce prepended.
- Applied at the repo boundary on declared fields (initially: `users.phone`, `users.display_name` if captured). Emails stay plaintext for lookup; revisit if needed.
- sqlc type overrides where a column maps cleanly; otherwise encrypt in service before repo call.

### RBAC (Casbin)
- Model: RBAC with domains — `g, user, role, org`; `p, role, org, obj, act`.
- Seeded policies: `owner` → all; `admin` → members.* read/write, invites.*; `member` → read.
- Policies stored in Postgres (`casbin_rule` table). Adapter: evaluate existing `casbin-pgx-adapter` at implementation time; if it drags a heavy dep tree, write a ~100-line `LoadPolicy/SavePolicy` against `pgx/v5` — decide once, in the plan.

### Observability
- `otelhttp` per request, `otelpgx` on the pool, otel propagator into asynq task payloads (traceparent header field) so worker spans join traces.
- `slog` JSON handler enriches every record with `request_id`, `trace_id`, `tenant_id` when present.
- Grafana Cloud OTLP endpoint + token from env; absent → `noop` provider (local dev costs nothing).

## 6. Auth & User Sync

- Supabase Auth owns sign-up/login/OAuth/reset. Backend verifies the access-token JWT via JWKS (`MicahParks/keyfunc` with cache + refresh).
- On every authenticated request, middleware lazy-upserts `users(id, email, last_seen)` from claims (`sub`, `email`). New users get a row on first request; email changes self-heal. No auth webhook to operate.
- `GET /v1/me` returns profile + org memberships.
- Dev seeds create users through Supabase Admin API using the service key (never shipped to clients).

## 7. Errors & Shutdown

- `errs`: `Problem` type (RFC 9457: `type`, `title`, `status`, `detail`, `invalid_params[]`).
- Services return wrapped sentinels: `ErrNotFound`→404, `ErrConflict`→409, `ErrValidation`→422 with validator field errors, `ErrUnauthorized`→401, `ErrForbidden`→403. Outer middleware maps; handlers don't hand-roll statuses.
- asynq handlers: default retry (exp backoff, 25 attempts); permanent failures (validation, unknown recipient) → `SkipRetry` + error log + optional DLQ task type.
- Shutdown (`SIGTERM`): `errgroup` sequence — http drain (10s) → watermill router close → asynq client close → pgx pool → otel flush. Worker proc drains asynq server independently.

## 8. Testing

- **Unit:** testify; service tests fake repo interfaces; pure domain logic needs no DB.
- **Integration:** testcontainers-go on Podman — requires `DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock` and `TESTCONTAINERS_RYUK_DISABLED=true` (documented in README/Makefile; CI uses Docker where both envs are absent/ignored).
  - Images: `postgres:16-alpine`, `redis:7-alpine` (local Redis stands in for Upstash — asynq/ratelimit/idempotency behave identically).
  - `goose` runs migrations against the container DB; RLS policies exercised — a test that bypasses `WithTenantTx` must see zero rows.
- **E2E:** `httptest.NewServer(router)` + full container stack; cover invite flow end-to-end including enqueued task (assert via asynq inspector or test subscriber).
- JWT in tests: sign tokens with a test key pair; `keyfunc` pointed at a test JWKS handler.

## 9. CI Pipeline (`.github/workflows/ci.yml`)

Jobs on GitHub-hosted runners:
1. `lint` — gofumpt check + golangci-lint (strict config).
2. `arch` — go-arch-lint boundary check (features → shared only).
3. `test` — Docker-based testcontainers, `go test -race ./...`.
4. `sec` — `govulncheck`.
5. `build` — `deploy/Containerfile` multi-stage build → scratch image (push to GHCR on `main` only).

## 10. Explicit Tradeoffs (accepted)

| Decision | Tradeoff |
|---|---|
| Manual `wire.go`, no fx | Startup panics become compile errors; loses lifecycle sugar — compensated with explicit errgroup shutdown |
| Watermill GoChannel, not Streams | At-most-once; durable work must go through asynq directly |
| Casbin in Postgres | +`casbin_rule` table, per-org enforcer cache — pays off when permissions grow past the 3 seeded roles |
| OTel everywhere | Wiring tax on every feature — pays off in agent-driven debugging across api/worker/DB |
| Lazy user upsert | Extra upsert per authenticated request (indexed, cheap) vs. operating an auth webhook |
| PII crypto at repo boundary | CPU cost trivial; key rotation needs a re-encrypt job when the time comes (not v1) |

## 11. Build Order (for the plan)

1. Skeleton: module, config, slog, errs, chi server, health check, Containerfile, CI lint job.
2. Database: pgxpool, goose migrations (`users`, `orgs`, `memberships`, `invites`, `casbin_rule`), sqlc.
3. Auth: JWKS verify middleware, lazy upsert, `/v1/me`.
4. Tenancy: `WithTenantTx`, RLS policies, tenant middleware.
5. Orgs: CRUD, members, invites, events, asynq email task, Casbin policies.
6. Infra: ratelimit, idempotency, otel, worker proc, seeds.
7. Billing stub: `plan` column + `Can()` entitlement fn.

Skipped from blueprint, deliberately: `pkg/`, feature flags, self-hosted runners, Redis Streams, fx, `Unit of Work` abstraction (real `pgx` txs via `WithTenantTx` instead).
