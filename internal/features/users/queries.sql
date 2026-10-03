-- sqlc queries for the users feature (Task 3 adds the real set).

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;
