-- plans: limits live in data so self-hosters can change them without recompiling
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE plans (
    id                     smallint PRIMARY KEY,
    name                   text NOT NULL UNIQUE,
    token_ttl_seconds      integer NOT NULL,
    max_archive_bytes      bigint NOT NULL,
    max_unpacked_bytes     bigint NOT NULL,
    max_files              integer NOT NULL,
    max_tokens_per_ip_hour integer NOT NULL,
    max_uploads_per_hour   integer NOT NULL,
    history_size           smallint NOT NULL DEFAULT 1
);

INSERT INTO plans (
    id, name, token_ttl_seconds, max_archive_bytes, max_unpacked_bytes,
    max_files, max_tokens_per_ip_hour, max_uploads_per_hour, history_size
) VALUES (
    1, 'anonymous', 1800, 104857600, 314572800,
    5000, 10, 60, 1
);

CREATE TABLE users (
    id            bigserial PRIMARY KEY,
    email         citext NOT NULL UNIQUE,
    password_hash text NOT NULL,
    plan_id       smallint NOT NULL REFERENCES plans(id),
    created_at    timestamptz NOT NULL DEFAULT now(),
    verified_at   timestamptz
);

CREATE TABLE tokens (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash     bytea NOT NULL UNIQUE,
    token_prefix   text NOT NULL,
    user_id        bigint REFERENCES users(id),
    plan_id        smallint NOT NULL REFERENCES plans(id),
    project_name   text,
    subdomain      text NOT NULL UNIQUE,
    created_at     timestamptz NOT NULL DEFAULT now(),
    expires_at     timestamptz NOT NULL,
    last_upload_at timestamptz,
    upload_count   integer NOT NULL DEFAULT 0,
    revision       integer NOT NULL DEFAULT 0,
    created_ip     inet NOT NULL,
    revoked_at     timestamptz,
    purged_at      timestamptz
);

CREATE INDEX tokens_expires_live_idx
    ON tokens (expires_at)
    WHERE purged_at IS NULL;

CREATE INDEX tokens_created_ip_created_at_idx
    ON tokens (created_ip, created_at);

CREATE TABLE builds (
    id             bigserial PRIMARY KEY,
    token_id       uuid NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
    revision       integer NOT NULL,
    archive_sha256 bytea NOT NULL,
    size_bytes     bigint NOT NULL,
    file_count     integer NOT NULL,
    uploaded_at    timestamptz NOT NULL DEFAULT now(),
    replaced_at    timestamptz,
    removed_at     timestamptz,
    UNIQUE (token_id, revision)
);

CREATE TABLE events (
    id          bigserial PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    event_type  text NOT NULL,
    token_id    uuid,
    properties  jsonb NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX events_occurred_at_idx ON events (occurred_at);
CREATE INDEX events_type_occurred_at_idx ON events (event_type, occurred_at);

CREATE TABLE rate_limit_buckets (
    bucket_key   text PRIMARY KEY,
    window_start timestamptz NOT NULL,
    counter      integer NOT NULL
);
