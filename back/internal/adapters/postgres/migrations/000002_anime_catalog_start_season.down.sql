BEGIN;

DROP INDEX IF EXISTS catalog_start_season_idx;

ALTER TABLE anime_catalog
    DROP CONSTRAINT IF EXISTS anime_catalog_start_season_name_check;

ALTER TABLE anime_catalog
    DROP COLUMN IF EXISTS start_season_name,
    DROP COLUMN IF EXISTS start_season_year;

COMMIT;
