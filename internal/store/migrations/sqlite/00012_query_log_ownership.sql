-- +goose Up

-- Who is running this query, and has anybody asked for it to stop.
--
-- The PostgreSQL copy of this migration carries the reasoning. The difference
-- here is mechanical: every added column has a constant default or is
-- nullable, so SQLite takes plain ADD COLUMNs and needs no table rebuild --
-- unlike 00011, which had to rebuild to add a CHECK.

ALTER TABLE query_log ADD COLUMN owner               TEXT NOT NULL DEFAULT '';
ALTER TABLE query_log ADD COLUMN heartbeat_at        TEXT;
ALTER TABLE query_log ADD COLUMN cancel_requested_at TEXT;
ALTER TABLE query_log ADD COLUMN cancel_requested_by TEXT;

CREATE INDEX query_log_owner_running_idx ON query_log (owner) WHERE state = 'running';

-- +goose Down

DROP INDEX IF EXISTS query_log_owner_running_idx;

ALTER TABLE query_log DROP COLUMN owner;
ALTER TABLE query_log DROP COLUMN heartbeat_at;
ALTER TABLE query_log DROP COLUMN cancel_requested_at;
ALTER TABLE query_log DROP COLUMN cancel_requested_by;
