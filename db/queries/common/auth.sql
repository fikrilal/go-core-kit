-- name: CreatePasswordCredential :exec
INSERT INTO password_credentials (user_id, password_hash, created_at, updated_at)
VALUES (sqlc.arg(user_id), sqlc.arg(password_hash), sqlc.arg(created_at), sqlc.arg(updated_at));

-- name: GetUserForLogin :one
SELECT
    u.id,
    u.email,
    u.email_verified_at,
    u.role,
    u.status,
    c.password_hash
FROM users AS u
LEFT JOIN password_credentials AS c ON c.user_id = u.id
WHERE u.email = sqlc.arg(email);

-- name: CreateSession :exec
INSERT INTO sessions (
    id, user_id, device_id, device_name, ip, user_agent, active_key,
    last_seen_at, expires_at, created_at, updated_at
) VALUES (
    sqlc.arg(id),
    sqlc.arg(user_id),
    NULLIF(sqlc.arg(device_id), ''),
    NULLIF(sqlc.arg(device_name), ''),
    NULLIF(sqlc.arg(ip), ''),
    NULLIF(sqlc.arg(user_agent), ''),
    NULLIF(sqlc.arg(active_key), ''),
    sqlc.arg(last_seen_at),
    sqlc.arg(expires_at),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (id, token_hash, expires_at, session_id, created_at, updated_at)
VALUES (
    sqlc.arg(id),
    sqlc.arg(token_hash),
    sqlc.arg(expires_at),
    sqlc.arg(session_id),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: RevokeActiveSessionTokens :execrows
UPDATE refresh_tokens
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(revoked_at)
WHERE revoked_at IS NULL
  AND session_id IN (
      SELECT id FROM sessions
      WHERE active_key = NULLIF(sqlc.arg(active_key), '')
        AND user_id = sqlc.arg(user_id)
        AND revoked_at IS NULL
  );

-- name: RevokeSessionByActiveKey :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at),
    active_key = NULL,
    push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(revoked_at),
    push_token_revoked_at = sqlc.arg(revoked_at),
    updated_at = sqlc.arg(revoked_at)
WHERE active_key = NULLIF(sqlc.arg(active_key), '')
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL;
