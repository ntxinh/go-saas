-- +goose Up
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
-- +goose Down
DROP TABLE memberships;
DROP TABLE orgs;
