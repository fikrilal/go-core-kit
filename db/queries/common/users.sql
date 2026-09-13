-- name: CreateUser :one
INSERT INTO users (id, email, created_at, updated_at)
VALUES (sqlc.arg(id), sqlc.arg(email), sqlc.arg(created_at), sqlc.arg(updated_at))
RETURNING id, email, created_at, updated_at;

-- name: GetUserByID :one
SELECT id, email, created_at, updated_at
FROM users
WHERE id = sqlc.arg(id);
