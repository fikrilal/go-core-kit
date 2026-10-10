-- +goose Up
ALTER TABLE users ADD COLUMN deletion_requested_at TEXT;
ALTER TABLE users ADD COLUMN deletion_scheduled_for TEXT;
ALTER TABLE users ADD COLUMN deletion_requested_session_id TEXT;
ALTER TABLE users ADD COLUMN deletion_requested_trace_id TEXT;

CREATE TABLE user_account_deletion_audits (
    id               TEXT PRIMARY KEY,
    actor_user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    actor_session_id TEXT NOT NULL,
    target_user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action           TEXT NOT NULL CHECK (action IN ('REQUESTED', 'CANCELED')),
    trace_id         TEXT,
    created_at       TEXT NOT NULL
);

CREATE INDEX user_account_deletion_audits_target_idx ON user_account_deletion_audits (target_user_id, created_at);
CREATE INDEX user_account_deletion_audits_actor_idx ON user_account_deletion_audits (actor_user_id, created_at);
CREATE INDEX user_account_deletion_audits_trace_idx ON user_account_deletion_audits (trace_id);

-- +goose Down
DROP INDEX user_account_deletion_audits_trace_idx;
DROP INDEX user_account_deletion_audits_actor_idx;
DROP INDEX user_account_deletion_audits_target_idx;
DROP TABLE user_account_deletion_audits;
ALTER TABLE users DROP COLUMN deletion_requested_trace_id;
ALTER TABLE users DROP COLUMN deletion_requested_session_id;
ALTER TABLE users DROP COLUMN deletion_scheduled_for;
ALTER TABLE users DROP COLUMN deletion_requested_at;
