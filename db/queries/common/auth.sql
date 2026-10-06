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
-- NULLIF stores a blank optional string as NULL.
-- A caller cannot persist an empty string for those columns.
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

-- name: GetRefreshTokenWithSession :one
SELECT
    t.id AS token_id,
    t.expires_at AS token_expires_at,
    t.revoked_at AS token_revoked_at,
    t.replaced_by_id AS token_replaced_by_id,
    s.id AS session_id,
    s.expires_at AS session_expires_at,
    s.revoked_at AS session_revoked_at,
    u.id AS user_id,
    u.email AS user_email,
    u.email_verified_at AS user_email_verified_at,
    u.role AS user_role,
    u.status AS user_status
FROM refresh_tokens AS t
JOIN sessions AS s ON s.id = t.session_id
JOIN users AS u ON u.id = s.user_id
WHERE t.token_hash = sqlc.arg(token_hash);

-- name: MarkRefreshTokenRotated :execrows
UPDATE refresh_tokens
SET revoked_at = sqlc.arg(revoked_at),
    replaced_by_id = NULLIF(sqlc.arg(replaced_by_id), ''),
    updated_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL AND replaced_by_id IS NULL;

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = sqlc.arg(last_seen_at),
    ip = NULLIF(sqlc.arg(ip), ''),
    user_agent = NULLIF(sqlc.arg(user_agent), ''),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: GetSessionIDByRefreshTokenHash :one
SELECT session_id FROM refresh_tokens WHERE token_hash = sqlc.arg(token_hash);

-- name: ClearSessionDeviceState :exec
UPDATE sessions
SET active_key = NULL,
    push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(now),
    push_token_revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: RevokeSession :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeSessionRefreshTokens :execrows
UPDATE refresh_tokens
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(revoked_at)
WHERE session_id = sqlc.arg(session_id) AND revoked_at IS NULL;

-- name: GetPasswordCredentialHash :one
SELECT password_hash FROM password_credentials WHERE user_id = sqlc.arg(user_id);

-- name: UpdatePasswordCredentialHash :exec
UPDATE password_credentials
SET password_hash = sqlc.arg(password_hash), updated_at = sqlc.arg(updated_at)
WHERE user_id = sqlc.arg(user_id);

-- name: RevokeOtherSessions :execrows
UPDATE sessions
SET revoked_at = sqlc.arg(revoked_at),
    active_key = NULL,
    push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(revoked_at),
    push_token_revoked_at = sqlc.arg(revoked_at),
    updated_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id)
  AND id != sqlc.arg(session_id)
  AND revoked_at IS NULL;

-- name: RevokeOtherSessionRefreshTokens :execrows
UPDATE refresh_tokens
SET revoked_at = sqlc.arg(revoked_at), updated_at = sqlc.arg(revoked_at)
WHERE revoked_at IS NULL
  AND session_id IN (
      SELECT s.id FROM sessions AS s
      WHERE s.user_id = sqlc.arg(user_id) AND s.id != sqlc.arg(session_id)
  );

-- name: GetUserIDByEmail :one
SELECT id FROM users WHERE email = sqlc.arg(email);

-- name: SetUserEmailVerified :exec
UPDATE users
SET email_verified_at = sqlc.arg(email_verified_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id);

-- name: GetUserByExternalIdentity :one
SELECT u.id, u.email, u.email_verified_at, u.role, u.status
FROM external_identities AS e
JOIN users AS u ON u.id = e.user_id
WHERE e.provider = sqlc.arg(provider) AND e.subject = sqlc.arg(subject);

-- name: CreateExternalIdentity :exec
INSERT INTO external_identities (id, provider, subject, email, user_id, created_at, updated_at)
VALUES (
    sqlc.arg(id),
    sqlc.arg(provider),
    sqlc.arg(subject),
    NULLIF(sqlc.arg(email), ''),
    sqlc.arg(user_id),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: GetExternalIdentityUserID :one
SELECT user_id FROM external_identities
WHERE provider = sqlc.arg(provider) AND subject = sqlc.arg(subject);

-- name: GetExternalIdentityIDByUserAndProvider :one
SELECT id FROM external_identities
WHERE user_id = sqlc.arg(user_id) AND provider = sqlc.arg(provider);

-- name: VerifyUserEmailIfUnset :execrows
UPDATE users
SET email_verified_at = sqlc.arg(email_verified_at), updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND email_verified_at IS NULL;

-- name: CreateUserProfile :exec
INSERT INTO user_profiles (user_id, display_name, given_name, family_name, created_at, updated_at)
VALUES (
    sqlc.arg(user_id),
    NULLIF(sqlc.arg(display_name), ''),
    NULLIF(sqlc.arg(given_name), ''),
    NULLIF(sqlc.arg(family_name), ''),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: GetMeView :one
SELECT
    u.id AS user_id,
    u.email AS user_email,
    u.email_verified_at AS user_email_verified_at,
    u.role AS user_role,
    u.status AS user_status,
    p.display_name AS profile_display_name,
    p.given_name AS profile_given_name,
    p.family_name AS profile_family_name
FROM users AS u
LEFT JOIN user_profiles AS p ON p.user_id = u.id
WHERE u.id = sqlc.arg(user_id);

-- name: GetAuthMethods :many
SELECT 'PASSWORD' AS method FROM password_credentials AS pc WHERE pc.user_id = sqlc.arg(user_id)
UNION
SELECT ei.provider AS method FROM external_identities AS ei WHERE ei.user_id = sqlc.arg(user_id);
