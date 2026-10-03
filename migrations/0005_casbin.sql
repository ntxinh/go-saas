-- +goose Up
-- casbin_rule is global: policies carry the org domain in v2, so no
-- tenant_id and no RLS. Reads/writes run as the pool owner.
CREATE TABLE casbin_rule (
  id serial PRIMARY KEY,
  ptype varchar(16) NOT NULL,
  v0 varchar(255) NOT NULL DEFAULT '',
  v1 varchar(255) NOT NULL DEFAULT '',
  v2 varchar(255) NOT NULL DEFAULT '',
  v3 varchar(255) NOT NULL DEFAULT '',
  v4 varchar(255) NOT NULL DEFAULT '',
  v5 varchar(255) NOT NULL DEFAULT '',
  UNIQUE (ptype, v0, v1, v2, v3, v4, v5));
-- +goose Down
DROP TABLE casbin_rule;
