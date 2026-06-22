BEGIN;

-- Collapse the franchise grouping from two tables (anime_franchises header +
-- anime_franchise_members) into a single anime_franchises table keyed by
-- anime_id. The surrogate franchise id and the derived member_key both go away:
-- a franchise is now identified by group_id, the smallest member id in the
-- group, which doubles as the representative on read (no MIN() lookup, no header
-- row). Dead service columns (member_key, created_at, updated_at) are dropped
-- with the old header.

-- Snapshot the existing grouping as (anime_id -> representative) pairs before
-- the old tables are dropped. group_id is the smallest member id of each
-- franchise.
CREATE TEMP TABLE franchise_merge ON COMMIT DROP AS
WITH reps AS (
    SELECT franchise_id, MIN(anime_id) AS group_id
    FROM anime_franchise_members
    GROUP BY franchise_id
)
SELECT m.anime_id, r.group_id
FROM anime_franchise_members m
JOIN reps r ON r.franchise_id = m.franchise_id;

DROP TABLE anime_franchise_members;
DROP TABLE anime_franchises;

-- Anime grouped into franchises. Each row maps a catalog entry to the franchise
-- it belongs to, identified by group_id -- the smallest member id in the
-- connected component of the relation graph (excluding character/other links).
-- group_id is also the franchise's representative on read, so neither a separate
-- header row nor a MIN() lookup is needed. A standalone title is its own one-row
-- group (group_id = anime_id). The franchise refresh recomputes these rows.
CREATE TABLE anime_franchises (
    anime_id INTEGER PRIMARY KEY REFERENCES anime_catalog(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL,
    CHECK (anime_id > 0),
    CHECK (group_id > 0),
    CHECK (group_id <= anime_id)
);

CREATE INDEX anime_franchises_group_idx
    ON anime_franchises (group_id, anime_id);

INSERT INTO anime_franchises (anime_id, group_id)
SELECT anime_id, group_id
FROM franchise_merge;

-- Drop the dead catalog timestamps and the index that sorted on updated_at: no
-- query reads either. details_synced_at (the catalog refresh cursor) stays.
DROP INDEX catalog_resolved_idx;

ALTER TABLE anime_catalog
    DROP COLUMN created_at;

ALTER TABLE anime_catalog
    DROP COLUMN updated_at;

COMMIT;
