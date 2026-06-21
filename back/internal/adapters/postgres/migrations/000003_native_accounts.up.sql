BEGIN;

-- Native account credentials. Both columns stay NULL for public-sync snapshot
-- rows and legacy MAL-only rows; they are populated only for accounts that
-- registered with email + password.
ALTER TABLE users
    ADD COLUMN email TEXT,
    ADD COLUMN password_hash TEXT;

-- Email uniqueness applies only to rows that actually have an email, so the
-- index is partial. Comparison is case-insensitive to match login lookups.
CREATE UNIQUE INDEX users_email_lower_idx
    ON users (LOWER(email)) WHERE email IS NOT NULL;

COMMIT;
