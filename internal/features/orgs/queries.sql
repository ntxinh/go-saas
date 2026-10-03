-- sqlc queries for the orgs feature. Tenant-scoped statements run inside
-- WithTenantTx (RLS filters by app.current_tenant); membership resolution
-- (MemberRole, ListUserOrgs) deliberately runs as the pool owner.

-- name: CreateOrg :one
INSERT INTO orgs(name) VALUES($1)
RETURNING tenant_id, name, plan, created_at;

-- name: GetOrg :one
SELECT tenant_id, name, plan, created_at FROM orgs WHERE tenant_id = $1;

-- name: UpdateOrg :one
UPDATE orgs SET name = $2 WHERE tenant_id = $1
RETURNING tenant_id, name, plan, created_at;

-- name: MemberRole :one
SELECT role FROM memberships WHERE tenant_id = $1 AND user_id = $2;

-- name: ListMembers :many
SELECT user_id, role, created_at FROM memberships
WHERE tenant_id = $1 ORDER BY created_at, user_id;

-- name: ListUserOrgs :many
SELECT m.tenant_id, o.name, m.role FROM memberships m
JOIN orgs o ON o.tenant_id = m.tenant_id
WHERE m.user_id = $1 ORDER BY o.name;

-- name: AddMember :exec
INSERT INTO memberships(tenant_id, user_id, role) VALUES($1, $2, $3);

-- name: RemoveMember :execrows
DELETE FROM memberships WHERE tenant_id = $1 AND user_id = $2;

-- name: ChangeRole :execrows
UPDATE memberships SET role = $3 WHERE tenant_id = $1 AND user_id = $2;

-- name: CountOwners :one
SELECT count(*) FROM memberships WHERE tenant_id = $1 AND role = 'owner';
