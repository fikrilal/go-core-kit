-- +goose Up
CREATE TABLE email_verification_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX email_verification_tokens_user_idx ON email_verification_tokens (user_id);
CREATE INDEX email_verification_tokens_expires_idx ON email_verification_tokens (expires_at);

CREATE TABLE password_reset_tokens (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX password_reset_tokens_user_idx ON password_reset_tokens (user_id);
CREATE INDEX password_reset_tokens_expires_idx ON password_reset_tokens (expires_at);

-- +goose Down
DROP TABLE password_reset_tokens;
DROP TABLE email_verification_tokens;
