-- name: TouchUser :execrows
UPDATE users
SET updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status != 'DELETED';

-- name: GetUserProfile :one
SELECT display_name, given_name, family_name
FROM user_profiles
WHERE user_id = sqlc.arg(user_id);

-- name: GetUserSession :one
SELECT id, user_id, revoked_at
FROM sessions
WHERE id = sqlc.arg(id);

-- name: UpsertUserProfile :exec
-- NULLIF stores a blank cleared field as NULL; a caller cannot persist an empty string.
INSERT INTO user_profiles (user_id, display_name, given_name, family_name, created_at, updated_at)
VALUES (
    sqlc.arg(user_id),
    NULLIF(sqlc.arg(display_name), ''),
    NULLIF(sqlc.arg(given_name), ''),
    NULLIF(sqlc.arg(family_name), ''),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
)
ON CONFLICT (user_id) DO UPDATE SET
    display_name = excluded.display_name,
    given_name = excluded.given_name,
    family_name = excluded.family_name,
    updated_at = excluded.updated_at;
