-- +goose Up
ALTER TABLE users ADD COLUMN deleted_at TEXT;

-- The audit action CHECK must admit the finalize outcomes, and SQLite cannot
-- alter a check: rebuild the table and copy the rows.
CREATE TABLE user_account_deletion_audits_old (
    id               TEXT PRIMARY KEY,
    actor_user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    actor_session_id TEXT NOT NULL,
    target_user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action           TEXT NOT NULL CHECK (action IN ('REQUESTED', 'CANCELED', 'FINALIZED', 'FINALIZE_BLOCKED_LAST_ADMIN')),
    trace_id         TEXT,
    created_at       TEXT NOT NULL
);

INSERT INTO user_account_deletion_audits_old (id, actor_user_id, actor_session_id, target_user_id, action, trace_id, created_at)
SELECT id, actor_user_id, actor_session_id, target_user_id, action, trace_id, created_at FROM user_account_deletion_audits;

DROP TABLE user_account_deletion_audits;
ALTER TABLE user_account_deletion_audits_old RENAME TO user_account_deletion_audits;

CREATE INDEX user_account_deletion_audits_target_idx ON user_account_deletion_audits (target_user_id, created_at);
CREATE INDEX user_account_deletion_audits_actor_idx ON user_account_deletion_audits (actor_user_id, created_at);
CREATE INDEX user_account_deletion_audits_trace_idx ON user_account_deletion_audits (trace_id);

-- +goose Down
DROP INDEX user_account_deletion_audits_trace_idx;
DROP INDEX user_account_deletion_audits_actor_idx;
DROP INDEX user_account_deletion_audits_target_idx;

CREATE TABLE user_account_deletion_audits_old (
    id               TEXT PRIMARY KEY,
    actor_user_id    TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    actor_session_id TEXT NOT NULL,
    target_user_id   TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    action           TEXT NOT NULL CHECK (action IN ('REQUESTED', 'CANCELED')),
    trace_id         TEXT,
    created_at       TEXT NOT NULL
);
INSERT INTO user_account_deletion_audits_old (id, actor_user_id, actor_session_id, target_user_id, action, trace_id, created_at)
SELECT id, actor_user_id, actor_session_id, target_user_id, action, trace_id, created_at FROM user_account_deletion_audits;
DROP TABLE user_account_deletion_audits;
ALTER TABLE user_account_deletion_audits_old RENAME TO user_account_deletion_audits;
CREATE INDEX user_account_deletion_audits_target_idx ON user_account_deletion_audits (target_user_id, created_at);
CREATE INDEX user_account_deletion_audits_actor_idx ON user_account_deletion_audits (actor_user_id, created_at);
CREATE INDEX user_account_deletion_audits_trace_idx ON user_account_deletion_audits (trace_id);

ALTER TABLE users DROP COLUMN deleted_at;
