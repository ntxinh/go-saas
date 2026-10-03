-- +goose Up
CREATE TABLE users (
  id uuid PRIMARY KEY, email text NOT NULL UNIQUE,
  display_name bytea, phone bytea,
  created_at timestamptz NOT NULL DEFAULT now(),
  last_seen timestamptz NOT NULL DEFAULT now());
-- +goose Down
DROP TABLE users;
