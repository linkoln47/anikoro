BEGIN;

DROP INDEX IF EXISTS users_email_lower_idx;

ALTER TABLE users
    DROP COLUMN IF EXISTS password_hash,
    DROP COLUMN IF EXISTS email;

COMMIT;
