-- sqlc queries for the users feature.

-- name: SyncUser :exec
INSERT INTO users(id, email) VALUES($1, $2)
ON CONFLICT (id) DO UPDATE SET email = $2, last_seen = now();

-- name: GetUser :one
SELECT id, email, display_name, phone, created_at FROM users WHERE id = $1;
