# go-saas v1 Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the go-saas B2B workspace backend skeleton through the orgs/invites/RBAC feature, with all v1 infra (events, jobs, rate-limit, idempotency, OTel) wired in.

**Architecture:** Feature-Sliced Design as vertical slices. Features under `internal/features/<name>` may import only `internal/shared/*`. Manual DI in `internal/app/wire.go`. Tenant isolation via Postgres RLS + `SET LOCAL app.current_tenant` inside `pgx` transactions, executed as non-owner role `app_user`.

**Tech Stack:** Go 1.24+, chi v5, pgx/v5, sqlc, goose, watermill (GoChannel), asynq, casbin, OTel→OTLP, slog, go-redis v9, testcontainers (Podman), testify, miniredis.

**Spec:** `docs/superpowers/specs/2026-10-03-go-saas-backend-design.md`

## Global Constraints

- Module path: `github.com/exodia/go-saas` (change everywhere if the remote differs).
- Go ≥ 1.24. Tool deps via `go get -tool` (sqlc, goose, gofumpt, golangci-lint, govulncheck, go-arch-lint).
- No `pkg/`, no `uber/fx`, no feature flags, no Stripe, no self-hosted CI runners.
- Redis namespaces: `idem:` idempotency, `rl:` rate-limit, `asynq` internal. One Upstash DB.
- Every commit must pass `gofumpt -l .` (empty), `go vet ./...`, `go build ./...`.
- Migrations run at API boot under a `pg_advisory_lock` via embedded FS (`migrations/` embedded). Testcontainers call the same `Migrate()`.
- **RLS model:** the pool connects as the migration/owner user. `WithTenantTx` runs `SET LOCAL ROLE app_user` + `SET LOCAL app.current_tenant`. `app_user` has NO `BYPASSRLS`. Queries as owner bypass RLS — that is correct for tenant-resolution and admin paths. Direct unscoped owner queries against tenant tables are a bug; reviewers reject them.
- Every tenant table column is named `tenant_id` (orgs uses `tenant_id` for its id too — consistent policy code).
- Error shape everywhere: RFC 9457 problem+json via `shared/errs`.
- Podman locally: `DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock`, `TESTCONTAINERS_RYUK_DISABLED=true`, `TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/run/user/$UID/podman/podman.sock`. CI uses Docker; keep envs unset there.
- Go toolchain pinned via `mise.toml` (`go = "1.24.x"`); `mise install` before first build. Tool deps still via `go get -tool`.
- TDD: failing test first for every behavior. Commit per task.

## File Map

```
cmd/api/main.go · cmd/worker/main.go · cmd/seed/main.go
internal/app/wire.go
internal/shared/config/config.go · config_test.go
internal/shared/errs/errs.go · errs_test.go
internal/shared/database/database.go · migrate.go · migrations/embed.go
internal/shared/server/server.go · respond.go · respond_test.go
internal/shared/middleware/{requestid,authn,tenant,rbac,ratelimit,idempotency}.go + tests
internal/shared/events/{events.go,payloads.go} · events_test.go
internal/shared/queue/{client.go,tasks.go} · tasks_test.go
internal/shared/otel/otel.go · otel_test.go
internal/testutil/postgres.go · redis.go
internal/shared/security/crypto.go · crypto_test.go
internal/features/auth/{feature.go,handler.go}
internal/features/users/{feature.go,handler.go,service.go,repo.go,sqlc/*}
internal/features/orgs/{feature.go,handler.go,service.go,repo.go,sqlc/*,events.go,tasks.go,worker_handlers.go,casbin_adapter.go,policies.go}
internal/features/billing/{entitlement.go,entitlement_test.go}
migrations/0001_roles.sql · 0002_users.sql · 0003_orgs.sql · 0004_invites.sql · 0005_casbin.sql
deploy/Containerfile · deploy/compose.yml
.env.example · Makefile · sqlc.yaml · .golangci.yml · .go-arch-lint.yml · casbin/model.conf
.github/workflows/ci.yml · README.md · DESIGN.md · AGENTS.md · .editorconfig · mise.toml · lsp.json
```

---

### Task 1: Module skeleton — config, errs, server, health

**Files:**
- Create: `go.mod` (`go mod init github.com/exodia/go-saas && go mod edit -go=1.24`)
- Create: `internal/shared/config/config.go`, `config_test.go`
- Create: `internal/shared/errs/errs.go`, `errs_test.go`
- Create: `internal/shared/server/respond.go`, `server.go`, `respond_test.go`
- Create: `cmd/api/main.go`
- Create: `.env.example`, `Makefile`, `.gitignore`, `deploy/Containerfile`
- Create: `mise.toml`, `.editorconfig`, `lsp.json`, `AGENTS.md`, `README.md`, `DESIGN.md`, `docs/` (dir — design docs index lives in `docs/README.md`; specs/plans stay under `docs/superpowers/`)

**Interfaces:**
- Produces:
  - `config.Load() (*Config, error)` — `caarlos0/env`; fields: `Port` (env `PORT`, default 8080), `Env` (`APP_ENV`, default `dev`), `DatabaseURL` (`DATABASE_URL`, required), `RedisURL` (`REDIS_URL`, required), `SupabaseURL` (`SUPABASE_URL`, required), `SupabaseServiceKey` (`SUPABASE_SERVICE_KEY`), `PIIKey` (`PII_KEY` hex 64 chars), `AppURL` (`APP_URL`, default `http://localhost:8080` — base for invite links), `OTLPEndpoint` (`OTEL_EXPORTER_OTLP_ENDPOINT`), `OTLPHeaders` (`OTEL_EXPORTER_OTLP_HEADERS`), `ResendAPIKey` (`RESEND_API_KEY`), `SMTPAddr` (`SMTP_ADDR`, default `localhost:1025`), `MailFrom` (`MAIL_FROM`, default `go-saas <no-reply@go-saas.local>`). Derived methods: `JWKsURL() string` → `SupabaseURL + "/auth/v1/.well-known/jwks.json"`, `Issuer() string` → `SupabaseURL + "/auth/v1"`.
  - `errs.Problem{Type,Title string; Status int; Detail string; InvalidParams []Param{Field,Reason string}}`, sentinels `ErrNotFound, ErrConflict, ErrValidation, ErrUnauthorized, ErrForbidden`, `errs.Write(w http.ResponseWriter, err error)`, `errs.Validation(fields map[string]string) error`.
  - `server.WriteJSON(w http.ResponseWriter, status int, v any)`.
  - `server.New(cfg *config.Config, log *slog.Logger) *chi.Mux` — mounts `GET /healthz` → `{"status":"ok"}`, `server.Run(ctx, r, addr)` using errgroup + `http.Server.Shutdown(10s)`.

- [ ] **Step 1: Failing tests**

```go
// internal/shared/errs/errs_test.go
func TestWriteMapsSentinels(t *testing.T) {
	cases := []struct{ err error; status int }{
		{fmt.Errorf("get org: %w", errs.ErrNotFound), 404},
		{errs.ErrConflict, 409}, {errs.ErrUnauthorized, 401},
		{errs.ErrForbidden, 403}, {errors.New("boom"), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		errs.Write(rec, c.err)
		assert.Equal(t, c.status, rec.Code)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/problem+json")
	}
}
func TestValidationDetail(t *testing.T) {
	rec := httptest.NewRecorder()
	errs.Write(rec, errs.Validation(map[string]string{"name": "required"}))
	assert.Equal(t, 422, rec.Code)
	var p errs.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	assert.Equal(t, "name", p.InvalidParams[0].Field)
}
```

```go
// internal/shared/config/config_test.go
func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := config.Load()
	assert.Error(t, err)
}
func TestDeriveJWKS(t *testing.T) {
	c := &config.Config{SupabaseURL: "https://x.supabase.co"}
	assert.Equal(t, "https://x.supabase.co/auth/v1/.well-known/jwks.json", c.JWKsURL())
}
```

```go
// internal/shared/server/respond_test.go
func TestHealthz(t *testing.T) {
	r := server.New(&config.Config{Port: 0}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	assert.Equal(t, 200, rec.Code)
}
```

- [ ] **Step 2: Run** `go test ./internal/shared/...` → FAIL (packages missing).

- [ ] **Step 3: Implement**

`errs/errs.go`: sentinels; `Write` does `errors.As`-style unwrapping: `errors.Is(err, ErrValidation)` → build Problem from stored params (`validationErr` type carrying `map[string]string`, sorted by field for determinism); else `errors.Is` each sentinel; default 500 `internal`. `WriteJSON` via `json.NewEncoder`, `Content-Type: application/problem+json`.

`config/config.go`: `env.Parse(&c)` + explicit required checks returning `fmt.Errorf("config: %w", err)`; hex-decode `PII_KEY` when set → error if not 32 bytes.

`server/respond.go`:
```go
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

`server/server.go`: `chi.NewRouter()`, `middleware.RequestID` (chi), mount `/healthz`. `Run(ctx, r, addr)`:
```go
srv := &http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 10 * time.Second}
g, ctx := errgroup.WithContext(ctx)
g.Go(func() error { return srv.ListenAndServe() })
g.Go(func() error { <-ctx.Done(); sctx, c := context.WithTimeout(context.Background(), 10*time.Second); defer c(); return srv.Shutdown(sctx) })
return g.Wait() // caller ignores http.ErrServerClosed
```

`cmd/api/main.go`: `config.Load()` → `slog.New(JSON(os.Stdout))` → `server.New` → `signal.NotifyContext(SIGTERM,SIGINT)` → `server.Run`. Fatal → `os.Exit(1)`.

`.env.example` lists all envs; `.gitignore`: `.env`, `bin/`, `coverage.out`. `Makefile` targets: `run test lint fmt migrate gen seed`. `deploy/Containerfile`:
```dockerfile
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG BIN=api
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/app ./cmd/$BIN
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/app /app
ENTRYPOINT ["/app"]
```

Repo hygiene files:

`mise.toml`:
```toml
[tools]
go = "1.24"
```

`.editorconfig`:
```ini
root = true
[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
indent_style = tab
indent_size = 4
[*.{yml,yaml,json,toml,sql,md}]
indent_style = space
indent_size = 2
```

`lsp.json` (omp `xd://lsp` config — gopls; verify on first `reload *` since the schema is tool-internal):
```json
{ "servers": { "go": { "command": "gopls", "args": ["serve"] } } }
```

`AGENTS.md` — the agent contract, ≤40 lines: module layout; boundary rule (features import only `internal/shared/*` + self — `go-arch-lint` enforces); tenant rule (all tenant-table queries via `database.WithTenantTx`; owner-conn queries to tenant tables are bugs); error rule (return sentinels, `errs.Write` at the edge); test rule (TDD; `internal/testutil` containers; Podman env vars from Makefile `podman-env`); commands (`mise install`, `make test|lint|gen|migrate`); never commit `.env`, never hand-edit `internal/features/*/sqlc` (generated).

`README.md` — quickstart: `mise install` → `cp .env.example .env` → `make up` (compose deps) → `make migrate run`; test with `make test` (auto Podman env); links to `DESIGN.md`, `docs/`, `docs/superpowers/specs/`.

`DESIGN.md` — human-readable summary of the approved spec: pipeline diagram (middleware order), tenancy/RLS model incl. `app_user` role rationale, events-vs-asynq contract (at-most-once rule), PII, casbin model. One page; details cite the spec file.

`docs/README.md` — index: `DESIGN.md`, spec, plan. `docs/` is the home for any future design docs.

- [ ] **Step 4: Run** `go test ./... && go build ./...` → PASS.
- [ ] **Step 5: Commit** `git add -A && git commit -m "feat: module skeleton — config, errs, server, health"`

---

### Task 2: Database — pool, goose migrations, `WithTx`/`WithTenantTx`, sqlc

**Files:**
- Create: `internal/shared/database/database.go`, `migrate.go`, `database_test.go` (container), `internal/shared/database/migrations/embed.go`
- Create: `migrations/0001_roles.sql`, `migrations/0002_users.sql`
- Create: `sqlc.yaml`
- Create: `internal/testutil/postgres.go` — testcontainer helper
- Modify: `Makefile` (+`db` target notes for Podman socket)

**Interfaces:**
- Produces:
  - `database.NewPool(ctx, url string) (*pgxpool.Pool, error)` — `pgxpool.ParseConfig`, `MaxConns` 10, `otelpgx` tracer appended in Task 10 (leave `cfg.ConnConfig.Tracer = nil` extension point documented).
  - `database.Migrate(ctx, url string) error` — advisory lock `pg_advisory_lock(727272)`, `goose.NewProvider(goose.DialectPostgreSQL, fs, nil)` on embedded `migrations`, `Up`.
  - `type TxFn func(tx pgx.Tx) error`; `database.WithTx(ctx, pool, fn) error`; `database.WithTenantTx(ctx, pool, tenantID uuid.UUID, fn TxFn) error` — begins tx, `SET LOCAL ROLE app_user`, `SET LOCAL app.current_tenant = $1`, runs fn, commit; rollback on error/panic-rethrow.
  - `testutil.Postgres(t) (url string)` — `postgres:16-alpine` container, runs `database.Migrate`, returns URL. Skips if `TESTCONTAINERS` env = `skip`.

- [ ] **Step 1: Failing test** (`database_test.go` — integration):

```go
func TestTenantTxSetsRoleAndGUC(t *testing.T) {
	url := testutil.Postgres(t)
	pool, err := database.NewPool(context.Background(), url)
	require.NoError(t, err)
	defer pool.Close()
	org := uuid.New()
	var role, guc string
	require.NoError(t, database.WithTenantTx(context.Background(), pool, org, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT current_user, current_setting('app.current_tenant', true)`).Scan(&role, &guc)
	}))
	assert.Equal(t, "app_user", role)
	assert.Equal(t, org.String(), guc)
}

func TestWithTxRollback(t *testing.T) {
	url := testutil.Postgres(t)
	pool, _ := database.NewPool(context.Background(), url)
	defer pool.Close()
	err := database.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		if _, e := tx.Exec(context.Background(), `INSERT INTO users(id,email) VALUES($1,$2)`, uuid.New(), "a@b.c"); e != nil { return e }
		return errors.New("boom")
	})
	require.Error(t, err)
	var n int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n))
	assert.Equal(t, 0, n)
}
```

(RLS visibility assertions live in Task 4, which creates the first RLS table.)

- [ ] **Step 2: Run** → FAIL (no package/migrations).

- [ ] **Step 3: Implement**

`0001_roles.sql`:
```sql
-- +goose Up
CREATE ROLE app_user NOLOGIN;
GRANT app_user TO CURRENT_USER;
GRANT USAGE ON SCHEMA public TO app_user;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;
-- +goose Down
REVOKE app_user FROM CURRENT_USER; DROP ROLE app_user;
```

`0002_users.sql` (`bytea` for PII):
```sql
-- +goose Up
CREATE TABLE users (
  id uuid PRIMARY KEY, email text NOT NULL UNIQUE,
  display_name bytea, phone bytea,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen timestamptz NOT NULL DEFAULT now());
```

`WithTenantTx`:
```go
func WithTenantTx(ctx context.Context, pool *pgxpool.Pool, tenantID uuid.UUID, fn TxFn) error {
	tx, err := pool.Begin(ctx)
	if err != nil { return err }
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL ROLE app_user`); err != nil { return err }
	if _, err = tx.Exec(ctx, `SELECT set_config('app.current_tenant',$1,true)`, tenantID); err != nil { return err }
	if err = fn(tx); err != nil { return err }
	return tx.Commit(ctx)
}
```

`sqlc.yaml` — one block per feature, schema `migrations/`, `sql_package: pgx/v5`, out `internal/features/<f>/sqlc`:
```yaml
version: "2"
sql:
  - engine: postgresql
    queries: internal/features/users/queries.sql
    schema: migrations/
    gen:
      go: { package: sqlc, out: internal/features/users/sqlc, sql_package: "pgx/v5" }
  - engine: postgresql
    queries: internal/features/orgs/queries.sql
    schema: migrations/
    gen:
      go: { package: sqlc, out: internal/features/orgs/sqlc, sql_package: "pgx/v5" }
```

`testutil.Postgres`: `testcontainers-go` postgres module, `postgres:16-alpine`, wait `ForSQL`, call `database.Migrate`. Document env in README/Make target `podman-env`:
```
export DOCKER_HOST=unix:///run/user/$UID/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED=true
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/run/user/$UID/podman/podman.sock
systemctl --user enable --now podman.socket
```

- [ ] **Step 4: Run** `go tool sqlc generate && go test ./internal/shared/database/` → PASS (container path needs `TESTCONTAINERS` not skipped).
- [ ] **Step 5: Commit** `git commit -m "feat: database pool, migrations, tenant tx, sqlc"`

---

### Task 3: Auth middleware — JWKS verify, lazy user upsert, `/v1/me`

**Files:**
- Create: `internal/features/users/queries.sql` (+ sqlc gen), `repo.go`, `service.go`, `service_test.go`
- Create: `internal/shared/middleware/authn.go`, `authn_test.go`, `ctx.go`
- Create: `internal/features/auth/feature.go`, `handler.go`, `handler_test.go`
- Create: `internal/shared/security/crypto.go`, `crypto_test.go`
- Modify: `internal/app/wire.go` (create — first slice), `cmd/api/main.go`

**Interfaces:**
- Produces:
  - `middleware.Authn(keys keyfunc.Keyfunc) func(http.Handler) http.Handler` — Bearer JWT → claims `sub uuid`, `email`; ctx key `ctxUserID`.
  - `middleware.UserID(ctx) (uuid.UUID, bool)`; `middleware.TenantID(ctx) (uuid.UUID, bool)` (set by Task 5).
  - `middleware.UpsertUser(svc *users.Service) func(http.Handler) http.Handler` — after Authn, `svc.Sync(ctx, sub, email)` (encryptable fields empty); on error → 500 problem.
  - `users.Service.Sync(ctx context.Context, id uuid.UUID, email string) error` — `INSERT ... ON CONFLICT (id) DO UPDATE email, last_seen`.
  - `users.Service.Profile(ctx, id) (users.Profile, error)` — decrypts PII fields.
  - `security.New(hexKey string) (*Cipher, error)`, `(*Cipher).Encrypt(plain string) ([]byte, error)`, `(*Cipher).Decrypt(blob []byte) (string, error)`, `Cipher.MayDecrypt(blob)` nil-safe for NULL columns.
  - `auth.Feature.RegisterRoutes(r *chi.Mux)` — `GET /v1/me` → `{id, email, orgs[]}` (orgs empty until Task 5; return `[]`).

- [ ] **Step 1: Failing tests**

```go
// crypto_test.go
func TestEncryptRoundTrip(t *testing.T) {
	c, _ := security.New(strings.Repeat("ab", 32))
	blob, _ := c.Encrypt("+49151...")
	got, _ := c.Decrypt(blob)
	assert.Equal(t, "+49151...", got)
}
func TestDecryptWrongKeyFails(t *testing.T) { /* second cipher errors on Decrypt */ }

// authn_test.go — ES256 keypair, httptest JWKS server returning JWK set,
// sign RS256/ES256 token via golang-jwt; assert 200 + claims in ctx vs 401 paths:
// no header, garbage token, wrong kid, expired.
func TestAuthnAcceptsValidToken(t *testing.T) { /* … */ }
func TestAuthnRejects(t *testing.T) { /* table: missing/malformed/expired → 401 problem+json */ }

// users service_test.go (container): Sync twice → row updated not duplicated; Profile decrypts.
// auth handler_test.go: fake service → GET /v1/me shape.
```

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**

`crypto.go`: AES-256-GCM; nonce 12B random prepended. `keyfunc` usage in `Authn`:
```go
k, err := keyfunc.NewDefault(ctx, []string{cfg.JWKsURL()}) // refresh on unknown kid
// middleware: jwt.Parse(tok, k.Keyfunc, jwt.WithValidMethods([]string{"RS256","ES256"}),
//   jwt.WithIssuer(cfg.Issuer()), jwt.WithAudience("authenticated"))
```
`UpsertUser` runs post-Authn. `users/queries.sql`:
```sql
-- name: SyncUser :exec
INSERT INTO users(id,email) VALUES($1,$2)
ON CONFLICT (id) DO UPDATE SET email=$2, last_seen=now();
-- name: GetUser :one
SELECT id,email,display_name,phone,created_at FROM users WHERE id=$1;
```
`wire.go` first cut: `config→log→pool→keyfunc→cipher→users.Service→auth.Feature`, mount under `/v1` with `Authn`+`UpsertUser` on authed group.

- [ ] **Step 4: Run** `go tool sqlc generate && go test ./...` → PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: JWT authn, user sync, PII cipher, /v1/me"`

---

### Task 4: Orgs schema + tenant middleware + org CRUD

**Files:**
- Create: `migrations/0003_orgs.sql`
- Create: `internal/features/orgs/queries.sql`, `repo.go`, `service.go`, `handler.go`, `feature.go`, tests
- Create: `internal/shared/middleware/tenant.go`, `tenant_test.go`
- Modify: `internal/app/wire.go`, `internal/features/auth/handler.go` (populate `orgs[]` from memberships)

**Interfaces:**
- Consumes: `WithTenantTx`, `UserID`, `errs`, `server.WriteJSON`.
- Produces:
  - `middleware.Tenant(check MembershipChecker, param string) func(http.Handler) http.Handler`; `type MembershipChecker interface { IsMember(ctx, orgID, userID uuid.UUID) (role string, ok bool) }` — check runs as pool owner (bypasses RLS, correct); sets `ctxTenantID`, `ctxOrgRole`.
  - `middleware.Role(ctx) (string, bool)`.
  - `orgs.Service`: `Create(ctx, userID, name) (Org, error)` (inserts org + owner membership in one `WithTx`), `Get/List/Update`, `Members(ctx, orgID)`, `AddMember/RemoveMember/ChangeRole`.
  - Routes: `POST /v1/orgs`, `GET /v1/orgs`, `Route /v1/orgs/{orgID}` → `GET/PATCH`, `GET /v1/orgs/{orgID}/members`, `POST .../members`, `DELETE .../members/{userID}`, `PATCH .../members/{userID}`.

- [ ] **Step 1: Failing tests**
  - `tenant_test.go`: fake checker → member passes w/ ctx set; non-member → 404 (not 403 — no existence leak).
  - `orgs` integration (container): create org → owner membership; second user `POST /orgs` → separate tenant; **RLS proof**: `WithTenantTx` as org A sees only A rows; memberships across orgs isolated. Duplicate member → 409. PATCH name.
  - `service_test` unit: `RemoveMember` of last owner → `ErrValidation`.

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**

`0003_orgs.sql`:
```sql
CREATE TABLE orgs (
  tenant_id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL, plan text NOT NULL DEFAULT 'free',
  created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE memberships (
  tenant_id uuid NOT NULL REFERENCES orgs(tenant_id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id),
  role text NOT NULL CHECK (role IN ('owner','admin','member')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, user_id));
ALTER TABLE orgs ENABLE ROW LEVEL SECURITY;
ALTER TABLE memberships ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_orgs ON orgs
  USING (tenant_id = nullif(current_setting('app.current_tenant', true),'')::uuid);
CREATE POLICY tenant_memberships ON memberships
  USING (tenant_id = nullif(current_setting('app.current_tenant', true),'')::uuid)
  WITH CHECK (tenant_id = nullif(current_setting('app.current_tenant', true),'')::uuid);
```
(Repeat the two-line `ENABLE`+policies pattern for `invites`, `casbin_rule` in later migrations; `users` stays unrestricted.)

- Service: all org reads after resolution go through `WithTenantTx`; `Create` uses `WithTx` (owner role: org has no tenant yet; membership insert explicit `tenant_id`). Last-owner guard: count owners in tx before delete.
- `auth/handler.go`: inject `MembershipChecker`+`Members`-list query → fill `orgs[{tenant_id,name,role}]`.

- [ ] **Step 4: Run** → PASS incl. RLS assertions.
- [ ] **Step 5: Commit** `git commit -m "feat: orgs, memberships, RLS tenant isolation"`

---

### Task 5: Events infra (watermill GoChannel) + invites + email task enqueue

**Files:**
- Create: `internal/shared/events/events.go`, `payloads.go`, `events_test.go`
- Create: `internal/shared/queue/client.go`, `tasks.go`, `tasks_test.go` (miniredis)
- Create: `internal/shared/mail/mail.go`, `resend.go`, `smtp.go`, `mail_test.go`
- Create: `migrations/0004_invites.sql`
- Create: `internal/features/orgs/events.go`, `tasks.go`, invite queries+service+routes additions, `service_invite_test.go`
- Modify: `internal/app/wire.go`

**Interfaces:**
- Produces:
  - `events.NewRouter(log) (*message.Router, error)` (GoChannel pubsub, `middleware.Recoverer`, `middleware.Retry{MaxRetries:3}`), `events.Publisher`/`events.Subscribe(router, topic, handler)`.
  - Payloads in `shared/events/payloads.go`: `MemberInvited{OrgID, OrgName, Email, Token, InviterName}`, `MemberRemoved{OrgID, UserID}`, `RoleChanged{OrgID, UserID, Role}`, `MemberJoined{OrgID, UserID, Role}` (published by Accept; no v1 subscriber — the publish side is the contract), `UserRegistered{UserID, Email}`.
  - `queue.NewClient(redisOpt) *asynq.Client`, `queue.Enqueue[T any](ctx, client, taskType, payload, opts...) error` — JSON marshals, injects `traceparent` from `otel.SpanFromContext` when recording.
  - Task types consts: `TaskEmailInvite = "email:invite"`, `TaskInviteExpirySweep = "org:invite-expiry"`.
  - `mail.Sender interface { Send(ctx, to, subject, html string) error }`; `NewResend(key)` (plain HTTP POST `https://api.resend.com/emails`); `NewSMTP(addr)` (Mailpit via `net/smtp`).
  - `orgs.Service.Invite(ctx, orgID, email, role) (Invite, error)` — pending invite + token `crypto/rand` 32B hex; publishes `MemberInvited`; route `POST /v1/orgs/{orgID}/invites`, `POST /v1/invites/{token}/accept`, `DELETE .../invites/{id}`.
  - `orgs.Service.Accept(ctx, token, userID) error` — resolve invite by token (owner conn — the token is the capability), then `WithTenantTx`: valid + `expires_at > now()` + `accepted_at IS NULL` + JWT email matches invite email → insert membership, set `accepted_at`, publish `MemberJoined`. Errors: missing/used → `ErrNotFound`/`ErrConflict`, expired → `ErrValidation{invite: "expired"}` → 422, email mismatch → `ErrForbidden`.

- [ ] **Step 1: Failing tests**
  - `events_test`: subscribe echo handler → publish → handler receives payload (channel sync, timeout 2s).
  - `tasks_test` (miniredis): `Enqueue` → asynq inspector sees task; traceparent key present when span active.
  - Invite integration: invite → accept → membership exists; expired invite → 410 problem; wrong email → 403; replay accept → 409.
  - `mail_test`: fake resend HTTP server asserts body; SMTP against in-process test listener optional — keep HTTP test only.

- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement**
  - `0004_invites.sql`: `invites(id uuid pk, tenant_id fk, email text, role text, token text unique, accepted_at timestamptz, expires_at timestamptz, created_at)` + RLS policy same pattern.
  - Invite token 7-day expiry; `Accept` resolves `orgID` from invite row (owner-conn lookup before tenant tx — safe: token is the capability).
  - Wire: watermill router started in `wire.go`; subscriber `MemberInvited → Enqueue(TaskEmailInvite, payload)`; scheduler registers `TaskInviteExpirySweep` cron `@hourly` (marks expired invites, owner-conn).
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: events bus, asynq enqueue, invites with email task"`

---

### Task 6: Worker process + Mailpit compose

**Files:**
- Create: `cmd/worker/main.go`
- Create: `internal/app/wire_worker.go` — worker graph: config→log→redis→mail.Sender→asynq server; handlers map.
- Create: `internal/features/orgs/worker_handlers.go`, `worker_handlers_test.go`
- Create: `deploy/compose.yml`
- Modify: `Makefile` (+`worker` target), `internal/app/wire.go` (add `otel.Propagator` into enqueue calls already done in Task 5)

**Interfaces:**
- Produces:
  - `HandleEmailInvite(sender mail.Sender, appURL string) asynq.HandlerFunc` — renders minimal HTML template (embedded `html/template`, org name + accept URL `appURL + "/invites/" + payload.Token`); malformed payload → `asynq.SkipRetry`.
  - `HandleInviteExpirySweep(svc *orgs.Service) asynq.HandlerFunc` — calls `svc.ExpireStale(ctx) (int, error)` which runs `DELETE FROM invites WHERE accepted_at IS NULL AND expires_at < now()` on the owner conn (bypasses RLS — intended for maintenance) and logs the count; test asserts the stale row is gone.
- [ ] **Step 1: Failing test**: `HandleEmailInvite` with fake Sender asserts to/subject/body contains token+org; bad payload → `SkipRetry`.
- [ ] **Step 2-3**: mux `asynq.NewMux()`, `HandleFunc` per task; `main.go` mirrors api but runs `asynq.NewServer` with `Queues: {"default": 1}`. `compose.yml`: `mailpit` (`1025`, `8025`), `postgres:16-alpine`, `redis:7-alpine` for full local dev.
- [ ] **Step 4: Run** worker handler test + `go build ./cmd/worker` → PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: worker proc, mail sender, compose stack"`

---

### Task 7: Casbin RBAC

**Files:**
- Create: `casbin/model.conf` (embed via `embed.FS`)
- Create: `migrations/0005_casbin.sql`
- Create: `internal/shared/middleware/rbac.go`, `internal/shared/middleware/rbac_test.go`
- Create: `internal/features/orgs/casbin_adapter.go`, `casbin_adapter_test.go`, `policies.go`
- Modify: `internal/app/wire.go` (enforcer per-request w/ 60s cache), route annotations
- Modify: `internal/shared/events/payloads.go` already has `RoleChanged`/`MemberRemoved` — wire subscriber → `enforcer.Invalidate(orgID)`

**Interfaces:**
- `rbac.go`: `middleware.RBAC(ef *Enforcer, obj, act string) func(http.Handler)` — `ef.Enforce(UserID, TenantID, obj, act)` → 403.
- `orgs.Enforcer` wraps `casbin.CachedEnforcer` keyed per org? Simpler: one enforcer, `LoadFilteredPolicy` per org w/ in-memory `sync.Map` cache 60s TTL, `Invalidate(orgID)` on events. `Enforce(sub, dom, obj, act)`.
- `model.conf`:
```
[request_definition]
r = sub, dom, obj, act
[policy_definition]
p = sub, dom, obj, act
[role_definition]
g = _, _, _
[policy_effect]
e = some(where (p.eft == allow))
[matchers]
m = g(r.sub, p.sub, r.dom) && r.dom == p.dom && keyMatch2(r.obj, p.obj) && regexMatch(r.act, p.act)
```
- Policies seed (`policies.go`, run at boot via `MergePolicies`): `p,owner,*,orgs,.*` per-org `g`-lines added on membership write; base role policies: `p, role:owner, ORG, *, .*` style — decide concrete: `g, alice, role:admin, org1` + `p, role:admin, org1, /v1/orgs/*, GET|POST`. Keep resources = route paths (`keyMatch2`), acts = HTTP methods.

- [ ] **Step 1: Failing tests**
  - `casbin_adapter_test` (container): save/load policy round-trip vs `casbin_rule` table.
  - `rbac_test`: enforcer seeded → owner allowed PATCH org, member denied POST invites, non-member denied all; cross-org denied.
  - E2E: member role → `POST /orgs/{id}/invites` → 403; promote → invalidate → 200.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — adapter ~120 lines (`LoadPolicy`, `SavePolicy`, `AddPolicy`, `RemovePolicy`, `RemoveFilteredPolicy` on `casbin_rule` cols `ptype,v0..v5`). `Enforcer` uses `LoadFilteredPolicy` keyed by org domain. Wrap org routes: `RBAC(ef, r.URL.Path, r.Method)` via route-level `r.With(...)`. Membership writes add/remove `g` lines + publish events; subscriber invalidates cache.
- [ ] **Step 4: Run** → PASS.
- [ ] **Step 5: Commit** `git commit -m "feat: casbin RBAC with domains, pg adapter, route enforcement"`

---

### Task 8: Rate-limit + idempotency middlewares

**Files:**
- Create: `internal/shared/middleware/ratelimit.go`, `ratelimit_test.go` (miniredis), `idempotency.go`, `idempotency_test.go`
- Modify: `internal/app/wire.go`, `cmd/api/main.go` middleware order per spec §4

**Interfaces:**
- `middleware.RateLimit(l *redis_rate.Limiter) func(http.Handler) http.Handler` — key `rl:{userID|ip}`; `PerMinute(120)` public, `PerSecond(10)` burst → 429 problem + `Retry-After`.
- `middleware.Idempotency(rdb *redis.Client) func(http.Handler)` — `Idempotency-Key` on POST/PATCH only; `idem:{user}:{method}:{path}:{key}` `SET NX EX 86400` value `processing`; on finish store `{status,body}`; replay → stored response + `Idempotent-Replay: true` header; in-flight → 409.

- [ ] **Step 1: Failing tests** (miniredis): N+1th request → 429. Idempotency: same key twice → identical body, second has replay header, handler executed once (counter); in-flight simulated via direct `SET processing` → 409; GET bypassed.
- [ ] **Step 2: Run** → FAIL.
- [ ] **Step 3: Implement** — `statusRecorder` wraps `ResponseWriter`. **Order note:** idempotency middleware sits inside `authn` (needs user key) but only on mutating routes — mount via `r.With(Idempotency).Post(...)`.
- [ ] **Step 4-5**: Run → PASS → `git commit -m "feat: rate limiting + idempotency middlewares"`

---

### Task 9: OTel — otelhttp, otelpgx, asynq propagation, slog ctx

**Files:**
- Create: `internal/shared/otel/otel.go`, `otel_test.go`
- Modify: `database.go` (`otelpgx.NewTracer()` in pool config), `server.go` (wrap mux `otelhttp.NewHandler`), `queue/tasks.go` (extract traceparent on worker side), `internal/app/wire*.go` (shutdown flush), `internal/shared/middleware/requestid.go` (slog attrs incl `trace_id`)

**Interfaces:**
- `otel.Setup(ctx, cfg) (shutdown func(context.Context) error, error)` — `otlptracehttp` w/ env-derived endpoint+headers; absent → `noop`. Service name `go-saas-{env}`.
- `requestid` middleware logger already exists → add `slog.Handler` wrapper extracting `otel.SpanContext` → `trace_id`,`span_id` fields on every record.

- [ ] **Step 1: Failing test**: span ctx → `otel.Setup` with no endpoint returns non-nil shutdown + noop works; slog record contains `trace_id` when span in ctx (custom `slogtest`-style capture handler).
- [ ] **Step 2-3**: implement wiring; worker `queue.Handler` middleware extracts `traceparent` → `otel.GetTextMapPropagator().Extract`.
- [ ] **Step 4-5**: `go test ./... && go build ./...` → PASS → `git commit -m "feat: otel tracing end-to-end, trace-aware logging"`

---

### Task 10: Seed cmd + billing stub + README + full CI

**Files:**
- Create: `cmd/seed/main.go`
- Create: `internal/features/billing/entitlement.go`, `entitlement_test.go`
- Create: `.golangci.yml`, `.go-arch-lint.yml`, `.github/workflows/ci.yml`, `README.md`
- Modify: `deploy/compose.yml` (add `api`, `worker` build targets optional)

**Interfaces:**
- `billing.Can(plan, feature string) bool` — static table: `free:{members:3, projects:1}`, `pro:{members:50, projects:100}`, `enterprise:{-1}`; used by `orgs.Invite` (member count vs plan) and future features.
- `seed`: flags `-n orgs`, `-users` — creates users via Supabase Admin API `POST /auth/v1/admin/users` (service key) OR direct `INSERT` when `-local` (test bypass); orgs+members+invites via service layer? Direct SQL is honest for seeds — use `pgx` inserts, gofakeit names.

- [ ] **Step 1: Failing tests**: `Can("free","members:4")=false`, `Can("pro","members:50")=true`, `enterprise` unlimited; Invite integration → 4th member on free → 422 problem.
- [ ] **Step 2-4**: implement + docs. `.golangci.yml`: enable `errcheck govet staticcheck unused gocritic revive gofumpt goimports misspell`. `.go-arch-lint.yml`: components `shared`, `features/*`, `app`, `cmd` — rule: features may depend only on `shared`+self+stdlib+vendor-deps. CI: lint→arch-lint→test(race, Docker)→govulncheck→build Containerfile; on `main` push→GHCR.
- [ ] **Step 5: Commit** `git commit -m "feat: seeds, plan entitlements, CI, docs"`

---

## Self-Review Notes (author)

- Spec coverage: pipeline order ✓ (T9 requestid exists T1, otel T9 wraps outermost — reorder at wire time: `otelhttp` outer, then requestid — noted in T9); RLS ✓ T4; idempotency ✓ T8; events at-most-once contract ✓ T5; PII ✓ T3; casbin ✓ T7 (adapter decision made: custom pgx, 120 lines — beats dep evaluation); seeds ✓ T10; billing stub ✓ T10; Containerfile ✓ T1; compose ✓ T6; CI ✓ T10.
- Type consistency: `WithTenantTx(ctx, pool, tenantID, fn)` uniform; `MembershipChecker` defined T4 consumed by auth T4-edit; `Sender` T5/T6 consistent; `Can` T10 used by T5 invite — note: Invite gets plan-limit check added in T10 (feature flag not needed).
- `app_user` needs `GRANT` on `casbin_rule` too — covered by `ALTER DEFAULT PRIVILEGES` (T2) since all tables created by owner get grants; verify in T5/T7 migration test.
- Open follow-up none-blocking: emails plaintext by design; `MemberJoined` event published, no v1 subscriber (documented, not dead code — the pub side is the contract).
