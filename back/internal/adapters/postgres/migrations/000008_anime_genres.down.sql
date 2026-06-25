BEGIN;

DROP INDEX IF EXISTS anime_genres_genre_idx;
DROP TABLE IF EXISTS anime_genres;
DROP TABLE IF EXISTS genres;

COMMIT;
