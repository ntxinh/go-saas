# Agent contract — go-saas

## Layout

Feature-sliced Go backend. `cmd/{api,worker,seed}` entrypoints;
`internal/app/wire.go` manual DI; `internal/shared/*` (config, database,
events, queue, middleware, plans, server, security, errs);
`internal/features/*` (auth, users, orgs). Migrations in `migrations/` (goose);
queries via sqlc into `internal/features/*/sqlc` — never hand-edit
generated files.

## Rules

- Boundary: a feature imports only `internal/shared/*` and itself —
  never another feature's internals. Cross-feature facts travel as
  `shared/events` payloads or `shared/queue` task types. `go-arch-lint`
  enforces.
- Tenant: all tenant-table queries run inside `database.WithTenantTx`
  (`SET LOCAL app.current_tenant`). Owner-conn queries to tenant tables
  are bugs — RLS returns zero rows.
- Errors: services return wrapped sentinels (`ErrNotFound`,
  `ErrConflict`, `ErrValidation`, `ErrUnauthorized`, `ErrForbidden`);
  handlers call `errs.Write` at the edge — never hand-roll statuses.
- Tests: TDD, failing test first. Container tests use `internal/testutil`
  (testcontainers on Podman); env vars from `make podman-env`, auto-set
  by `make test`.

## Commands

`mise install` · `make test` · `make lint` · `make arch` · `make sec` · `make fmt` ·
`make gen` · `make migrate` · `make up` (compose deps) · `make up-app`.

## Never

Commit `.env`. Hand-edit `internal/features/*/sqlc`. Query tenant tables
outside `WithTenantTx`. Send the Supabase service key to clients.
