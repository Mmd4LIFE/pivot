-- +goose Up

-- The query log: what was run, by whom, and what happened.
--
-- Written in two phases rather than one at the end. A row appears when the
-- query starts and is completed when it finishes, because a log written only
-- on completion cannot answer the two questions people actually have: "what is
-- running right now" (Part 22's monitor) and "what was running when the
-- process died". A query that never finishes is exactly the one worth seeing.
--
-- `state` carries that: running, succeeded, failed, canceled. A row left
-- running by a crash is not a lie -- it is the last thing Pivot knew, and an
-- operator can tell it apart from a query still going by its started_at.
--
-- The SQL text is stored. It has to be: "which query is hammering the
-- warehouse" is unanswerable without it, and so is "what did this person run
-- before they left". It is also the reason this table needs a retention policy
-- before it needs an index -- Phase 9 owns that, and the note is here so the
-- growth is a decision rather than a surprise.
--
-- user_id is nullable because not every query has a person behind it. A
-- scheduled refresh runs as Pivot, and recording a fabricated user would make
-- the log's own audit value worse than leaving the truth absent.

CREATE TABLE query_log (
    id            UUID        NOT NULL PRIMARY KEY,
    org_id        UUID        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    connection_id UUID        NOT NULL,

    user_id       UUID,

    sql_text      TEXT        NOT NULL,

    state         TEXT        NOT NULL DEFAULT 'running',

    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    duration_ms   BIGINT      NOT NULL DEFAULT 0,

    rows_returned BIGINT      NOT NULL DEFAULT 0,

    -- An estimate of the result's size in Pivot's memory, not bytes on the
    -- wire: the driver has already decoded them by the time anything here can
    -- count. Useful for "which query is enormous" and not for billing.
    bytes_estimated BIGINT    NOT NULL DEFAULT 0,

    truncated     BOOLEAN     NOT NULL DEFAULT FALSE,

    -- Part 21 fills this in. 'uncached' until then, which is true rather than
    -- absent: nothing is consulting a cache yet.
    cache_status  TEXT        NOT NULL DEFAULT 'uncached',

    error_message TEXT        NOT NULL DEFAULT '',

    CONSTRAINT query_log_connection_fk
        FOREIGN KEY (connection_id, org_id) REFERENCES connections (id, org_id) ON DELETE CASCADE,

    CONSTRAINT query_log_state_check
        CHECK (state IN ('running', 'succeeded', 'failed', 'canceled')),

    CONSTRAINT query_log_org_key UNIQUE (id, org_id)
);

-- Newest first within an organization, which is every listing this has.
CREATE INDEX query_log_org_started_idx ON query_log (org_id, started_at DESC);

-- What Part 22's monitor asks: what is running now.
CREATE INDEX query_log_running_idx ON query_log (org_id, started_at) WHERE state = 'running';

-- +goose Down

DROP TABLE query_log;
