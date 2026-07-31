-- Auth without email verification: username + password.
-- email stays optional for a future mail provider.

INSERT INTO plans (
    id, name, token_ttl_seconds, max_archive_bytes, max_unpacked_bytes,
    max_files, max_tokens_per_ip_hour, max_uploads_per_hour, history_size
) VALUES (
    2, 'registered', 7200, 104857600, 314572800,
    5000, 30, 120, 1
) ON CONFLICT (id) DO NOTHING;

ALTER TABLE users ADD COLUMN IF NOT EXISTS username citext;
UPDATE users SET username = ('user_' || id::text)::citext WHERE username IS NULL;
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS users_username_key ON users (username);

ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

CREATE TABLE IF NOT EXISTS sessions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_ip  inet
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions (user_id);
CREATE INDEX IF NOT EXISTS sessions_expires_live_idx
    ON sessions (expires_at)
    WHERE revoked_at IS NULL;
