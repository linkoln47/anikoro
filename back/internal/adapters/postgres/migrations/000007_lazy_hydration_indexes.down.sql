BEGIN;

DROP INDEX IF EXISTS catalog_stale_idx;
DROP INDEX IF EXISTS catalog_unresolved_idx;

COMMIT;
