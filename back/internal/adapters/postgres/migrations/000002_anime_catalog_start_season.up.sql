BEGIN;

ALTER TABLE anime_catalog
    ADD COLUMN start_season_year SMALLINT,
    ADD COLUMN start_season_name TEXT;

ALTER TABLE anime_catalog
    ADD CONSTRAINT anime_catalog_start_season_name_check
    CHECK (
        start_season_name IS NULL
        OR start_season_name IN ('winter', 'spring', 'summer', 'fall')
    );

CREATE INDEX catalog_start_season_idx
    ON anime_catalog (start_season_year, start_season_name);

COMMIT;
