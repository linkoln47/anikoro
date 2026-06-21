BEGIN;

-- Reverse the MAL identity split. Note: rows deleted by the up migration
-- (public-sync shells) and the original MAL usernames cannot be restored; the
-- down migration only rebuilds the column shape, folding mal_user_id back into
-- users and re-keying mal_tokens by user_id.
ALTER TABLE users
    ADD COLUMN mal_user_id BIGINT;

UPDATE users u
SET mal_user_id = p.mal_user_id
FROM mal_profiles p
WHERE p.user_id = u.id;

CREATE UNIQUE INDEX users_mal_user_id_idx
    ON users (mal_user_id);

-- Re-key mal_tokens back to users(id).
ALTER TABLE mal_tokens
    ADD COLUMN user_id INTEGER;

UPDATE mal_tokens t
SET user_id = p.user_id
FROM mal_profiles p
WHERE p.id = t.mal_profile_id;

DELETE FROM mal_tokens WHERE user_id IS NULL;

ALTER TABLE mal_tokens DROP COLUMN mal_profile_id;

ALTER TABLE mal_tokens
    ALTER COLUMN user_id SET NOT NULL,
    ADD PRIMARY KEY (user_id),
    ADD CONSTRAINT mal_tokens_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE;

DROP TABLE mal_profiles;

COMMIT;
