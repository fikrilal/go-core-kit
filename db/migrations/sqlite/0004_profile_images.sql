-- +goose Up
CREATE TABLE stored_files (
    id            TEXT PRIMARY KEY,
    owner_user_id TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose       TEXT NOT NULL CHECK (purpose IN ('PROFILE_IMAGE')),
    status        TEXT NOT NULL DEFAULT 'UPLOADING' CHECK (status IN ('UPLOADING', 'ACTIVE', 'DELETED')),
    bucket        TEXT NOT NULL,
    object_key    TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL,
    uploaded_at   TEXT,
    deleted_at    TEXT,
    trace_id      TEXT,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    UNIQUE (bucket, object_key)
);

CREATE INDEX stored_files_owner_idx ON stored_files (owner_user_id, created_at);

-- A UNIQUE constraint cannot be added by ALTER TABLE, so the uniqueness of an
-- attached image is a separate index. Multiple NULLs stay legal.
ALTER TABLE user_profiles ADD COLUMN profile_image_file_id TEXT REFERENCES stored_files (id) ON DELETE SET NULL;
CREATE UNIQUE INDEX user_profiles_profile_image_idx ON user_profiles (profile_image_file_id);

-- +goose Down
DROP INDEX user_profiles_profile_image_idx;
ALTER TABLE user_profiles DROP COLUMN profile_image_file_id;
DROP INDEX stored_files_owner_idx;
DROP TABLE stored_files;
