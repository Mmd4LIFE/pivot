-- +goose Up

-- Drop the enumeration of connector kinds.
--
-- 00006 wrote `CHECK (kind IN ('postgres'))` and its comment called the
-- resulting migration-per-connector deliberate: "a row naming a connector that
-- does not exist is a connection nobody can open and nobody can explain."
--
-- The reasoning was sound and the mechanism is not. Part 18-a shipped the
-- MySQL connector without this migration, so a MySQL connection could be
-- configured, tested and then refused by the database on the way in. Nothing
-- caught it, because the connector's own tests never reached storage. That is
-- the cost of the constraint paid in full, on the very first connector after
-- the one it was written for.
--
-- The deeper problem is that this is not a fact about the data. Which
-- connectors exist is a property of the *binary*: ADR-0004 puts DuckDB behind
-- a `nocgo` build tag, so two Pivots built from the same commit will disagree
-- about which kinds are valid, and a schema cannot be right for both. A
-- constraint that has to be wrong for somebody is not integrity.
--
-- So the check becomes what is actually true at this layer -- a connection has
-- some kind -- and which kinds exist stays where it is knowable: the registry
-- in internal/connectors, which the CLI already checks against and which lists
-- exactly what this build can open.

ALTER TABLE connections DROP CONSTRAINT connections_kind_check;

ALTER TABLE connections
    ADD CONSTRAINT connections_kind_check CHECK (kind <> '');

-- +goose Down

ALTER TABLE connections DROP CONSTRAINT connections_kind_check;

ALTER TABLE connections
    ADD CONSTRAINT connections_kind_check CHECK (kind IN ('postgres'));
