BEGIN;

DROP INDEX IF EXISTS catalog_mal_score_idx;

ALTER TABLE anime_catalog
    DROP CONSTRAINT IF EXISTS anime_catalog_mal_score_check;

ALTER TABLE anime_catalog
    DROP COLUMN IF EXISTS mal_score;

COMMIT;
