BEGIN;

-- Community mean score from MAL (the `mean` field on the anime endpoint). NULL
-- means MAL has no score yet (unaired or too few ratings); it is never 0. The
-- per-anime score is the only stored rating: the "all anime" franchise rating is
-- derived on read as AVG(mal_score) over a franchise's members, so it stays in
-- sync with this column automatically and needs no separate storage.
ALTER TABLE anime_catalog
    ADD COLUMN mal_score NUMERIC(4, 2);

ALTER TABLE anime_catalog
    ADD CONSTRAINT anime_catalog_mal_score_check
    CHECK (mal_score IS NULL OR (mal_score >= 0 AND mal_score <= 10));

CREATE INDEX catalog_mal_score_idx
    ON anime_catalog (mal_score DESC NULLS LAST);

COMMIT;
