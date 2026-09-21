-- +goose Up

-- The SQLite mirror. See the postgres/ copy for the design commentary; this
-- file records only what differs, which is the usual type mapping.

CREATE TABLE sessions (
    id                  TEXT PRIMARY KEY,
    org_id              TEXT NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id             TEXT NOT NULL,
    token_hash          TEXT NOT NULL,
    issued_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at          TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    last_seen_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ip                  TEXT NOT NULL DEFAULT '',
    user_agent          TEXT NOT NULL DEFAULT '',
    revoked_at          TEXT,
    FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE
) STRICT;

CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE INDEX sessions_org_id_idx ON sessions (org_id);

CREATE INDEX sessions_absolute_expires_at_idx ON sessions (absolute_expires_at);

CREATE TABLE login_attempts (
    id              TEXT    PRIMARY KEY,
    org_id          TEXT    NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email           TEXT    NOT NULL,
    failed_count    INTEGER NOT NULL DEFAULT 0,
    first_failed_at TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_failed_at  TEXT    NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    locked_until    TEXT
) STRICT;

CREATE UNIQUE INDEX login_attempts_org_email_key ON login_attempts (org_id, email);

CREATE INDEX login_attempts_last_failed_at_idx ON login_attempts (last_failed_at);

-- +goose Down

DROP TABLE login_attempts;
DROP TABLE sessions;
