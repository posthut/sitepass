DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS users_username_key;
ALTER TABLE users DROP COLUMN IF EXISTS username;
-- email NOT NULL restoration skipped: unsafe if null rows exist
DELETE FROM plans WHERE id = 2 AND name = 'registered';
