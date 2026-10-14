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

-- name: TouchSessionPushToken :execrows
UPDATE sessions
SET push_token_updated_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id)
  AND user_id = sqlc.arg(user_id)
  AND revoked_at IS NULL
  AND expires_at > sqlc.arg(now);

-- name: ClearOtherSessionPushToken :execrows
UPDATE sessions
SET push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(now),
    push_token_revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE push_token = NULLIF(sqlc.arg(push_token), '') AND id != sqlc.arg(id);

-- name: SetSessionPushToken :exec
UPDATE sessions
SET push_platform = NULLIF(sqlc.arg(push_platform), ''),
    push_token = NULLIF(sqlc.arg(push_token), ''),
    push_token_updated_at = sqlc.arg(now),
    push_token_revoked_at = NULL,
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id);

-- name: ClearSessionPushToken :execrows
UPDATE sessions
SET push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(now),
    push_token_revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);

-- name: GetSessionForPush :one
SELECT s.id, s.push_token, s.revoked_at, s.expires_at, u.status AS user_status
FROM sessions AS s
JOIN users AS u ON u.id = s.user_id
WHERE s.id = sqlc.arg(id);

-- name: ClearSessionPushTokenIfMatches :execrows
UPDATE sessions
SET push_platform = NULL,
    push_token = NULL,
    push_token_updated_at = sqlc.arg(now),
    push_token_revoked_at = sqlc.arg(now),
    updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND push_token = NULLIF(sqlc.arg(push_token), '');

-- name: CreateStoredFile :exec
INSERT INTO stored_files (id, owner_user_id, purpose, status, bucket, object_key, content_type, size_bytes, trace_id, created_at, updated_at)
VALUES (
    sqlc.arg(id),
    sqlc.arg(owner_user_id),
    'PROFILE_IMAGE',
    'UPLOADING',
    sqlc.arg(bucket),
    sqlc.arg(object_key),
    sqlc.arg(content_type),
    sqlc.arg(size_bytes),
    NULLIF(sqlc.arg(trace_id), ''),
    sqlc.arg(created_at),
    sqlc.arg(updated_at)
);

-- name: GetStoredFileForOwner :one
SELECT id, status, object_key, content_type, size_bytes
FROM stored_files
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: MarkStoredFileActive :execrows
UPDATE stored_files
SET status = 'ACTIVE', uploaded_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: MarkStoredFileDeleted :execrows
UPDATE stored_files
SET status = 'DELETED', deleted_at = sqlc.arg(now), updated_at = sqlc.arg(now)
WHERE id = sqlc.arg(id) AND owner_user_id = sqlc.arg(owner_user_id);

-- name: GetUserProfileImageFileID :one
SELECT profile_image_file_id FROM user_profiles WHERE user_id = sqlc.arg(user_id);

-- name: SetUserProfileImageFileID :exec
INSERT INTO user_profiles (user_id, profile_image_file_id, created_at, updated_at)
VALUES (sqlc.arg(user_id), NULLIF(sqlc.arg(file_id), ''), sqlc.arg(now), sqlc.arg(now))
ON CONFLICT (user_id) DO UPDATE SET
    profile_image_file_id = excluded.profile_image_file_id,
    updated_at = excluded.updated_at;

-- name: ClearUserProfileImageFileID :execrows
UPDATE user_profiles
SET profile_image_file_id = NULL, updated_at = sqlc.arg(now)
WHERE user_id = sqlc.arg(user_id);

-- name: GetCurrentProfileImageFile :one
SELECT f.id, f.object_key
FROM user_profiles AS p
JOIN stored_files AS f ON f.id = p.profile_image_file_id
WHERE p.user_id = sqlc.arg(user_id);

-- name: GetUserDeletionState :one
SELECT role, status, deletion_requested_at, deletion_scheduled_for
FROM users
WHERE id = sqlc.arg(id);

-- name: CountActiveAdmins :one
SELECT COUNT(*) FROM users WHERE role = 'ADMIN' AND status = 'ACTIVE';

-- name: SetUserDeletionRequest :execrows
UPDATE users
SET deletion_requested_at = sqlc.arg(requested_at),
    deletion_scheduled_for = sqlc.arg(scheduled_for),
    deletion_requested_session_id = NULLIF(sqlc.arg(session_id), ''),
    deletion_requested_trace_id = NULLIF(sqlc.arg(trace_id), ''),
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status != 'DELETED';

-- name: ClearUserDeletionRequest :execrows
UPDATE users
SET deletion_requested_at = NULL,
    deletion_scheduled_for = NULL,
    deletion_requested_session_id = NULL,
    deletion_requested_trace_id = NULL,
    updated_at = sqlc.arg(updated_at)
WHERE id = sqlc.arg(id) AND status != 'DELETED';

-- name: CreateAccountDeletionAudit :exec
INSERT INTO user_account_deletion_audits (id, actor_user_id, actor_session_id, target_user_id, action, trace_id, created_at)
VALUES (
    sqlc.arg(id),
    sqlc.arg(actor_user_id),
    sqlc.arg(actor_session_id),
    sqlc.arg(target_user_id),
    sqlc.arg(action),
    NULLIF(sqlc.arg(trace_id), ''),
    sqlc.arg(created_at)
);

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
