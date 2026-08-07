-- One row per store, holding the same JSON document the file backend
-- writes. See docs/decisions/postgres-backend.md for why this is a blob
-- table rather than six relational schemas.
CREATE TABLE IF NOT EXISTS store_blob (
    -- 'auth', 'tokens', 'flags', 'entities', 'mac_registry',
    -- 'rule_usage', 'detector_settings'.
    name       text        PRIMARY KEY,
    -- text, not jsonb: jsonb reorders keys and rewrites number formats,
    -- and the bytes here have to round-trip exactly so one marshal path
    -- serves both backends and migration is byte-identical.
    payload    text        NOT NULL,
    -- Optimistic-concurrency token. Every write is conditional on this,
    -- which closes the lost-update window the file backend has between
    -- the running server and a CLI command.
    version    bigint      NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
