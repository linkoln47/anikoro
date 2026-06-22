BEGIN;

-- Partial indexes backing the lazy-worker's two scan queues. The split sync
-- model leaves new or incomplete catalog entries as resolved=false stubs for the
-- worker to hydrate, and the worker also re-fetches resolved entries whose
-- details (and thus mal_score) have gone stale.

-- Pass A queue: unresolved stubs the worker must hydrate, scanned oldest id
-- first (see ListUnresolvedCatalogIDs).
CREATE INDEX catalog_unresolved_idx
    ON anime_catalog (id) WHERE resolved = false;

-- Pass B queue: resolved entries ordered by how stale their details are, matching
-- ListStaleCatalogIDs (ORDER BY details_synced_at ASC NULLS FIRST, id ASC).
CREATE INDEX catalog_stale_idx
    ON anime_catalog (details_synced_at NULLS FIRST, id) WHERE resolved = true;

COMMIT;
