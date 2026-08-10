-- Watchlist matches (#243 slice 6): a dedicated, indexed table rather
-- than a row in store_blob. See docs/decisions/postgres-backend.md §1a
-- for why this doesn't reopen 0001's blob-table decision -- every store
-- in that migration keeps its whole dataset in memory and never issues
-- a query; this one deliberately does neither, which is exactly the
-- premise §1a scopes around it.
CREATE TABLE IF NOT EXISTS match_log (
    id                  text        PRIMARY KEY,
    entry_id            text        NOT NULL,
    -- entryID + source-identity-key + destIP + port -- the same
    -- collapsing key internal/matchlog.Tuple.key computes for the file
    -- backend. The unique index below is what makes a repeat of an
    -- already-open tuple an atomic update-in-place rather than a race
    -- between a lookup and an insert.
    tuple_key           text        NOT NULL,
    -- MAC-preferred, IP-fallback identity key ("mac:aa:bb:.." or
    -- "ip:1.2.3.4"), computed in Go (Identity.identityKey) rather than
    -- derived here, so the matching rule lives in exactly one place.
    -- What Query filters on.
    source_identity_key text        NOT NULL,
    source_mac          text        NOT NULL DEFAULT '',
    source_ip           text        NOT NULL DEFAULT '',
    dest_ip             text        NOT NULL,
    port                integer     NOT NULL,
    -- The full matched event, JSON-encoded -- text, not jsonb, same
    -- byte-identical round-trip reasoning as store_blob.payload.
    event               text        NOT NULL,
    first_seen          timestamptz NOT NULL,
    last_seen           timestamptz NOT NULL,
    count               bigint      NOT NULL DEFAULT 1
);

CREATE UNIQUE INDEX IF NOT EXISTS match_log_tuple_key ON match_log (tuple_key);

-- Serves Query: WHERE source_identity_key = $1 ... ORDER BY last_seen DESC.
CREATE INDEX IF NOT EXISTS match_log_source_last_seen ON match_log (source_identity_key, last_seen DESC);

-- Serves the retention purge: WHERE last_seen < $1.
CREATE INDEX IF NOT EXISTS match_log_last_seen ON match_log (last_seen);
