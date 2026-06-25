BEGIN;

-- Genre dimension. id is the MAL genre id, which is stable and globally unique on
-- MAL, so it is supplied by the hydrator rather than generated. name is MAL's
-- label. MAL's public anime `genres` field is a single flat list mixing genres,
-- themes, and demographics; they are all stored uniformly here.
CREATE TABLE genres (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    CHECK (id > 0),
    CHECK (name <> '')
);

-- Per-anime genre membership. A two-column junction between a catalog entry and
-- the genres MAL assigns to it. It is rebuilt from MAL on each detail hydration,
-- mirroring how anime_relations is replaced.
CREATE TABLE anime_genres (
    anime_id INTEGER NOT NULL REFERENCES anime_catalog(id) ON DELETE CASCADE,
    genre_id INTEGER NOT NULL REFERENCES genres(id) ON DELETE RESTRICT,
    PRIMARY KEY (anime_id, genre_id),
    CHECK (anime_id > 0),
    CHECK (genre_id > 0)
);

-- Reverse lookup ("which anime have genre X") backing the seasonal genre filter.
CREATE INDEX anime_genres_genre_idx
    ON anime_genres (genre_id, anime_id);

COMMIT;
