-- +goose Up

-- Sessions are server-side. Stateless tokens are deliberately not used for
-- browser sessions: they cannot be revoked before expiry, and "remove this
-- person's access" has to mean now. See
-- docs/architecture/security-model.md#sessions-are-server-side-deliberately.
--
-- token_hash, never the token. A database dump or a leaked backup must not
-- hand the reader a set of working sessions.
--
-- Two expiries, because they answer different questions. expires_at is the
-- idle timeout and is pushed forward on use; absolute_expires_at is a hard cap
-- that is never extended, so an active session still ends eventually. With
-- only the idle timeout, a stolen token stays valid forever as long as the
-- thief keeps using it.
--
-- The foreign key is composite on (user_id, org_id), per the lesson of
-- migration 00002: a single-column key would let a session name one
-- organization while pointing at another's user.

CREATE TABLE sessions (
    id                  UUID        PRIMARY KEY,
    org_id              UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    user_id             UUID        NOT NULL,
    token_hash          TEXT        NOT NULL,
    issued_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ NOT NULL,
    absolute_expires_at TIMESTAMPTZ NOT NULL,
    last_seen_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip                  TEXT        NOT NULL DEFAULT '',
    user_agent          TEXT        NOT NULL DEFAULT '',
    revoked_at          TIMESTAMPTZ,
    CONSTRAINT sessions_user_fkey
        FOREIGN KEY (user_id, org_id) REFERENCES users (id, org_id) ON DELETE CASCADE
);

-- Lookup is always by token hash, and it must be unique: two sessions sharing
-- a hash would mean a collision in the token generator.
CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);

CREATE INDEX sessions_user_id_idx ON sessions (user_id);

CREATE INDEX sessions_org_id_idx ON sessions (org_id);

-- Supports the expiry sweep.
CREATE INDEX sessions_absolute_expires_at_idx ON sessions (absolute_expires_at);

-- Failed login tracking, for progressive lockout.
--
-- Keyed by the ATTEMPTED email rather than by user_id, because a row must
-- exist even when the account does not. Locking out only real accounts is what
-- turns a lockout into a user-enumeration oracle: an attacker learns which
-- addresses exist by seeing which ones start refusing.
--
-- Rows for addresses that never existed are pruned by the same sweep that
-- clears expired sessions, so a dictionary attack cannot grow this table
-- without bound.

CREATE TABLE login_attempts (
    id              UUID        PRIMARY KEY,
    org_id          UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    email           TEXT        NOT NULL,
    failed_count    BIGINT      NOT NULL DEFAULT 0,
    first_failed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_failed_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_until    TIMESTAMPTZ
);

CREATE UNIQUE INDEX login_attempts_org_email_key ON login_attempts (org_id, email);

CREATE INDEX login_attempts_last_failed_at_idx ON login_attempts (last_failed_at);

-- +goose Down

DROP TABLE login_attempts;
DROP TABLE sessions;
