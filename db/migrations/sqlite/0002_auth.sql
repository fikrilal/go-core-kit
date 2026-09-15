-- +goose Up
ALTER TABLE users ADD COLUMN email_verified_at TEXT;
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'USER' CHECK (role IN ('USER', 'ADMIN'));
ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'SUSPENDED', 'DELETED'));
ALTER TABLE users ADD COLUMN suspended_at TEXT;
ALTER TABLE users ADD COLUMN suspended_reason TEXT;

CREATE TABLE password_credentials (
    user_id       TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    id                    TEXT PRIMARY KEY,
    user_id               TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    device_id             TEXT,
    device_name           TEXT,
    push_platform         TEXT CHECK (push_platform IN ('ANDROID', 'IOS', 'WEB')),
    push_token            TEXT UNIQUE,
    push_token_updated_at TEXT,
    push_token_revoked_at TEXT,
    ip                    TEXT,
    user_agent            TEXT,
    active_key            TEXT UNIQUE,
    last_seen_at          TEXT NOT NULL,
    expires_at            TEXT NOT NULL,
    revoked_at            TEXT,
    created_at            TEXT NOT NULL,
    updated_at            TEXT NOT NULL
);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);
CREATE INDEX sessions_device_id_idx ON sessions (device_id);
CREATE INDEX sessions_revoked_at_idx ON sessions (revoked_at);
CREATE INDEX sessions_push_token_revoked_at_idx ON sessions (push_token_revoked_at);

CREATE TABLE refresh_tokens (
    id             TEXT PRIMARY KEY,
    token_hash     TEXT NOT NULL UNIQUE,
    expires_at     TEXT NOT NULL,
    revoked_at     TEXT,
    session_id     TEXT NOT NULL REFERENCES sessions (id) ON DELETE CASCADE,
    replaced_by_id TEXT UNIQUE REFERENCES refresh_tokens (id),
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);

CREATE INDEX refresh_tokens_session_id_idx ON refresh_tokens (session_id);
CREATE INDEX refresh_tokens_revoked_at_idx ON refresh_tokens (revoked_at);

-- +goose Down
DROP TABLE refresh_tokens;
DROP TABLE sessions;
DROP TABLE password_credentials;

-- SQLite cannot drop CHECK-constrained columns; rebuild users to its 0001 shape.
CREATE TABLE users_old (
    id         TEXT PRIMARY KEY,
    email      TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
INSERT INTO users_old (id, email, created_at, updated_at)
SELECT id, email, created_at, updated_at FROM users;
DROP TABLE users;
ALTER TABLE users_old RENAME TO users;
