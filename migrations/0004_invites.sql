-- +goose Up
CREATE TABLE invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL REFERENCES orgs(tenant_id) ON DELETE CASCADE,
  email text NOT NULL,
  role text NOT NULL CHECK (role IN ('admin','member')),
  token text NOT NULL UNIQUE,
  accepted_at timestamptz,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX invites_tenant_pending ON invites(tenant_id) WHERE accepted_at IS NULL;
ALTER TABLE invites ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_invites ON invites
  USING (tenant_id = nullif(current_setting('app.current_tenant', true),'')::uuid)
  WITH CHECK (tenant_id = nullif(current_setting('app.current_tenant', true),'')::uuid);
-- +goose Down
DROP TABLE invites;
