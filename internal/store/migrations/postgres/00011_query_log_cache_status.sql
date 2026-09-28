-- +goose Up

-- Constrain cache_status to the vocabulary that means something.
--
-- 00010 shipped the column with a default and no check, while `state` next to
-- it has one. That asymmetry reads as deliberate to anybody who finds it -- as
-- though cache_status were an open field somebody may extend -- and it is not:
-- there are exactly three answers to "did the cache help", and a fourth
-- spelling of one of them is a typo.
--
-- The column is the input to the hit rate. A typo does not fail anything; it
-- quietly moves queries out of the numerator of the number an operator uses to
-- decide whether the cache is worth its memory.

ALTER TABLE query_log
    ADD CONSTRAINT query_log_cache_status_check
    CHECK (cache_status IN ('hit', 'miss', 'uncached'));

-- +goose Down

ALTER TABLE query_log DROP CONSTRAINT query_log_cache_status_check;
