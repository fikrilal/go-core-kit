-- +goose Up
CREATE TABLE user_profiles (
    user_id      TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    display_name TEXT,
    given_name   TEXT,
    family_name  TEXT,
    created_at   TIMESTAMPTZ NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL
);

CREATE TABLE external_identities (
    id         TEXT PRIMARY KEY,
    provider   TEXT NOT NULL CHECK (provider IN ('GOOGLE')),
    subject    TEXT NOT NULL,
    email      TEXT,
    user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (provider, subject),
    UNIQUE (user_id, provider)
);

CREATE INDEX external_identities_user_id_idx ON external_identities (user_id);

-- +goose Down
DROP TABLE external_identities;
DROP TABLE user_profiles;
