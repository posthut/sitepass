# Sitepass — Technical Specification

Version: 0.1 (MVP scope)
Platform: Debian 13, amd64
Licence intent: source-available, self-hostable

---

## 1. Purpose and scope

Sitepass publishes a static site build on a temporary public URL so that
a person working with a coding agent can look at the result.

The user creates a token, hands it to their agent, and the agent uploads a
build over HTTP. Anonymous use needs no account. Optional username+password
accounts (no email verification) get a longer token TTL and a list of previews.

### 1.1 In scope for MVP

- Anonymous token creation
- Optional registration/login without email (username + password, session cookie)
- Artifact upload via HTTP API, repeatable while the token is alive
- Publication on a dedicated subdomain with TLS
- Fixed lifetime, automatic deletion, disk high-water / critical capacity control
- Web interface in Russian, Kazakh and English
- Agent instruction block and machine-readable contract at `/llms.txt`
- Self-hosting via a public repository and a single bootstrap script

### 1.2 Out of scope for MVP

- Email verification or password recovery mail
- Build history beyond the live revision (`history_size` remains 1)
- Named project workspaces beyond the signed-in token list
- Server-side rendering, API routes, any execution of customer code
- Dependency installation
- Custom domains
- Payment

### 1.3 Non-goals, permanently

Sitepass does not build projects and does not diagnose why a customer's
build is wrong. It accepts an artifact that meets a stated contract and
serves it.

---

## 2. Actors

| Actor | Interaction |
|---|---|
| Human ("vibe coder") | Creates a token in the browser, copies the agent instruction, opens the preview URL |
| Agent | Reads the instruction, builds and packs the project, calls the upload API, reports the URL back |
| Operator | Deploys and runs an instance; may be the same person self-hosting |

The human is not assumed to be a developer. The agent is assumed to be a
language model with shell access.

---

## 3. Primary scenario

1. Human opens the control site and presses **Create upload token**.
2. Optionally enters a project name. It affects only the subdomain label
   and the page title.
3. The service returns: the token, the preview URL, the expiry time.
4. Human presses **Copy instruction for agent** and pastes it into the
   agent's chat.
5. Agent builds the project, packs the build output, uploads it.
6. Agent reports the URL. The human opens it and sees the site.
7. Human requests a change. Agent rebuilds and re-uploads with the same
   token. The URL does not change; the human refreshes the page.
8. At expiry the content is deleted and the URL returns a branded expiry
   page.

The preview URL is issued at step 3, before any upload. Opening it before
the first upload shows a waiting page.

---

## 4. Token model

| Property | Value |
|---|---|
| Entropy | 32 bytes from a cryptographic RNG |
| Encoding | Base32 without padding, prefixed `pv_` |
| Storage | SHA-256 only; plaintext is returned once and never persisted |
| Lifetime | Fixed, from creation. Default 1800 s |
| Uploads | Unlimited while alive |
| Revisions kept | One. Each upload replaces the previous build |
| Revocation | The holder may delete the token and its content at any time |

Rejected uploads do not affect the token in any way.

The token is a bearer credential that will be pasted into an agent chat
and will appear in that provider's logs. This is acceptable given the
lifetime and the absence of personal data. Compromise allows an attacker
to replace the content of one short-lived preview.

### 4.1 Subdomain label

    <slug>-<10 chars base32>

`slug` is derived from the project name: lowercased, transliterated to
ASCII, non-alphanumerics collapsed to hyphens, truncated to 24 characters.
Empty or unusable names fall back to a generated two-word label.

The slug carries no entropy and is not a secret. All unguessability lives
in the random suffix (50 bits). The label is validated against
`^[a-z0-9][a-z0-9-]{1,40}[a-z0-9]$` and checked for uniqueness among live
tokens.

A reserved-label list prevents collisions with operational names:
`www`, `api`, `admin`, `mail`, `static`, `assets`, `cdn`, `status`.

---

## 5. Data model

PostgreSQL 17. All timestamps `timestamptz`, stored in UTC.

### 5.1 plans

Limits live in data, not in code constants, so that a self-hoster can
change them without recompiling and so that registration can be added
later by inserting a row.

    id                      smallint primary key
    name                    text not null unique
    token_ttl_seconds       integer not null
    max_archive_bytes       bigint not null
    max_unpacked_bytes      bigint not null
    max_files               integer not null
    max_tokens_per_ip_hour  integer not null
    max_uploads_per_hour    integer not null
    history_size            smallint not null default 1

Seed row: `anonymous`, 1800 s, 100 MB, 300 MB, 5000 files, 10 tokens/hour,
60 uploads/hour, history 1.

### 5.2 users

Created in the initial migration, unused in MVP.

    id            bigserial primary key
    email         citext not null unique
    password_hash text not null
    plan_id       smallint not null references plans(id)
    created_at    timestamptz not null default now()
    verified_at   timestamptz

### 5.3 tokens

    id             uuid primary key default gen_random_uuid()
    token_hash     bytea not null unique
    token_prefix   text not null
    user_id        bigint references users(id)
    plan_id        smallint not null references plans(id)
    project_name   text
    subdomain      text not null unique
    created_at     timestamptz not null default now()
    expires_at     timestamptz not null
    last_upload_at timestamptz
    upload_count   integer not null default 0
    revision       integer not null default 0
    created_ip     inet not null
    revoked_at     timestamptz
    purged_at      timestamptz

    index on (expires_at) where purged_at is null
    index on (created_ip, created_at)

`user_id` is nullable. An anonymous token is a token with no user. Adding
registration requires no data migration.

### 5.4 builds

One live row per token, plus the row being replaced during the grace
period.

    id            bigserial primary key
    token_id      uuid not null references tokens(id) on delete cascade
    revision      integer not null
    archive_sha256 bytea not null
    size_bytes    bigint not null
    file_count    integer not null
    uploaded_at   timestamptz not null default now()
    replaced_at   timestamptz
    removed_at    timestamptz

    unique (token_id, revision)

### 5.5 events

Analytics foundation. Append-only, no updates.

    id          bigserial primary key
    occurred_at timestamptz not null default now()
    event_type  text not null
    token_id    uuid
    properties  jsonb not null default '{}'

    index on (occurred_at)
    index on (event_type, occurred_at)

No IP addresses, user agents or request bodies are stored in `properties`.
Aggregate counters only.

### 5.6 rate_limit_buckets

    bucket_key   text primary key
    window_start timestamptz not null
    counter      integer not null

Fixed windows, one hour. Survives restart, which an in-process counter
would not.

### 5.7 Retention

`tokens` and `builds` rows are kept for 30 days after purge for
operational forensics, then deleted by the reaper. `events` are kept for
365 days.

---

## 6. HTTP API

Base path `/api/v1`. All responses `application/json; charset=utf-8`
except `/llms.txt`.

### 6.1 Envelope

Success responses contain the payload at the top level with `"ok": true`.
Failure responses:

    {
      "ok": false,
      "error": {
        "code": "archive_too_large",
        "message": "Archive is 142 MB. The limit is 100 MB.",
        "details": {},
        "token_consumed": false,
        "docs": "https://sitepass.tech/llms.txt#archive_too_large"
      }
    }

`token_consumed` is always present on upload errors so that an agent never
has to infer retryability from the status code.

`message` is written in the language negotiated from `Accept-Language`,
defaulting to English. Machine logic must key on `code`, never on
`message`.

### 6.2 POST /api/v1/tokens

Public. Rate limited per IP.

Request:

    { "project_name": "landing" }      // optional, max 48 chars

Response 201:

    {
      "ok": true,
      "token": "pv_k7m3x9q2h8n4c6v1b5z0",
      "token_id": "9f1c...",
      "preview_url": "https://landing-k7m3x9q2h8.sitepass-app.tech",
      "expires_at": "2026-07-31T15:42:00Z",
      "expires_in_seconds": 1800,
      "limits": {
        "max_archive_bytes": 104857600,
        "max_unpacked_bytes": 314572800,
        "max_files": 5000
      }
    }

The token appears in this response only.

### 6.3 POST /api/v1/upload

Header: `Authorization: Bearer <token>`

Body: `multipart/form-data` with a single part named `archive`, or
`application/octet-stream` with the raw archive.

Optional header `X-Content-SHA256` — hex digest of the body. When present
it is verified before unpacking.

Response 200:

    {
      "ok": true,
      "preview_url": "https://landing-k7m3x9q2h8.sitepass-app.tech",
      "revision": 3,
      "expires_at": "2026-07-31T15:42:00Z",
      "expires_in_seconds": 1245,
      "file_count": 47,
      "size_bytes": 2841923,
      "warnings": [
        {
          "code": "missing_assets",
          "message": "index.html references 2 files that are not in the archive.",
          "details": { "paths": ["/assets/hero.webp", "/fonts/inter.woff2"] }
        }
      ]
    }

Warnings never block publication.

Concurrent uploads on one token: the second receives 409
`upload_in_progress`. Implemented with `SELECT ... FOR UPDATE NOWAIT` on
the token row.

### 6.4 GET /api/v1/status

Header: `Authorization: Bearer <token>`

    {
      "ok": true,
      "preview_url": "...",
      "expires_at": "...",
      "expires_in_seconds": 1245,
      "revision": 3,
      "has_build": true,
      "upload_count": 3
    }

Used by the control UI to drive the countdown and by agents to confirm
state.

### 6.5 DELETE /api/v1/token

Header: `Authorization: Bearer <token>`

Revokes the token and removes the content immediately. Response 204.

### 6.6 GET /api/v1/health

Public, unauthenticated.

    {
      "ok": true,
      "status": "healthy",
      "accepting_uploads": true,
      "disk_usage_percent": 41
    }

`status` is one of `healthy`, `degraded`, `read_only`. An agent can check
this before attempting a large upload.

### 6.7 GET /llms.txt

Plain text, no markup. Contains the artifact contract, the endpoint list,
every error code with its meaning, and two worked examples. Served with
`Cache-Control: public, max-age=300`.

This file is the primary documentation for agents and must be kept in sync
with the implementation by a test that compares the error code list in the
file against the codes registered in the code.

---

## 7. Archive intake

### 7.1 Accepted formats

| Format | Detection |
|---|---|
| `.tar.gz` | gzip magic, then tar |
| `.zip` | PK magic |
| `.tar.zst` | zstd magic, then tar |
| single `.html` | HTML content type or `<` first non-space byte |

Format is detected from content, not from the filename.

A single HTML file is published as `index.html`. Agents frequently produce
a self-contained page with CDN scripts, and requiring an archive for one
file is friction with no benefit.

### 7.2 Limits

| Limit | Default |
|---|---|
| Compressed size | 100 MB |
| Uncompressed size | 300 MB |
| File count | 5000 |
| Compression ratio | 200:1 |
| Path depth | 32 |
| Path segment length | 255 bytes |
| Total path length | 4096 bytes |

Limits are read from the plan row, not from constants.

### 7.3 Entry validation

An entry is rejected — and the whole upload with it — if any of:

- The path, after cleaning, escapes the destination root
- The path is absolute or contains a `..` segment
- The path contains a null byte or a backslash
- The entry type is not a regular file or a directory
  (symlinks, hardlinks, devices, FIFOs, sockets are all refused)
- The running uncompressed total exceeds the limit
- The running file count exceeds the limit

Rejection is fail-closed: partial content is never published. The staging
directory is removed.

Unpacking uses a bounded reader per entry. The limit is enforced during
the read, not checked against a declared header value, because the header
is attacker-controlled.

After unpacking, the staging root is resolved with symlink evaluation and
verified to be inside the expected parent. This is a second, independent
check; the first is per-entry validation.

### 7.4 Entry point resolution

1. If `index.html` exists at the archive root, that root is the site.
2. Otherwise, if the archive root contains exactly one directory and no
   files, descend into it and repeat once. This handles `dist/` being
   packed instead of its contents.
3. Otherwise, reject with `entrypoint_not_found`.

Descent is limited to one level. Deeper guessing would be diagnosing the
customer's packing mistake.

### 7.5 Warnings

Non-blocking observations, reported and then forgotten:

| Code | Condition |
|---|---|
| `missing_assets` | `index.html` references files absent from the archive. Up to 10 paths listed |
| `large_bundle` | A single file exceeds 2 MB |
| `source_tree_present` | `node_modules/`, `src/` or `.git/` present in the published tree |

No warning suggests a fix or names a build tool.

---

## 8. Error codes

`token_consumed` is `false` for every code in this table. Rejections never
consume anything.

| Code | HTTP | Meaning |
|---|---|---|
| `token_not_found` | 404 | Unknown token |
| `token_expired` | 410 | Lifetime elapsed |
| `token_revoked` | 410 | Deleted by its holder |
| `upload_in_progress` | 409 | Another upload is being processed for this token |
| `archive_too_large` | 413 | Compressed size over limit |
| `archive_unpacked_too_large` | 413 | Uncompressed size over limit |
| `archive_too_many_files` | 413 | File count over limit |
| `archive_malformed` | 422 | Not readable as the detected format |
| `archive_unsafe_entry` | 422 | Path traversal, symlink, or non-regular entry |
| `unsupported_format` | 415 | Not tar.gz, zip, tar.zst or html |
| `entrypoint_not_found` | 422 | No `index.html` at the root or one level down |
| `checksum_mismatch` | 422 | `X-Content-SHA256` does not match the body |
| `project_name_invalid` | 422 | Name is too long or produces an empty slug |
| `rate_limited` | 429 | Per-IP or per-token limit. Includes `Retry-After` |
| `storage_capacity_exceeded` | 503 | Disk above the high-water mark |
| `service_read_only` | 503 | Maintenance mode |
| `internal_error` | 500 | Unexpected. Includes a correlation id |

Message text is a statement of fact. It states what happened and, where
applicable, the observed and permitted values. It does not give advice
about build tooling.

Acceptable: `Archive contains 8,412 files. The limit is 5,000.`
Not acceptable: `Too many files — try excluding node_modules from your build.`

---

## 9. Capacity control

Two thresholds on the build filesystem, both configurable:

| Threshold | Default | Effect |
|---|---|---|
| High-water | 80 % | Token creation returns `storage_capacity_exceeded`. Existing tokens continue to work, including uploads |
| Critical | 90 % | The reaper additionally removes the oldest live tokens until usage falls below the high-water mark. Affected tokens are marked revoked |

Both values and current usage are exposed on `/api/v1/health`.

Rationale for the split: refusing new tokens degrades acquisition; killing
live previews degrades people mid-task. The cheaper failure comes first.

---

## 10. Publication and serving

### 10.1 Layout

    /srv/sitepass/builds/<token_id>/
        rev-N/
        current -> rev-N

Ownership `root:sitepass-content`. Directories `0555`, files `0444`.
The mount carries `nodev,nosuid,noexec` and a filesystem quota.

### 10.2 nginx preview vhost

    map $http_accept $sitepass_html_fallback {
        default        =404;
        "~*text/html"  /index.html;
    }

    server {
        listen 443 ssl;
        http2 on;
        server_name ~^(?<build_label>[a-z0-9-]+)\.PREVIEW_DOMAIN$;

        root /srv/sitepass/builds/$build_label/current;

        location / {
            try_files $uri $uri/ $uri.html $sitepass_html_fallback;
            autoindex off;
            add_header Cache-Control "no-cache" always;
            add_header X-Content-Type-Options "nosniff" always;
            add_header X-Robots-Tag "noindex, nofollow" always;
            etag on;
        }

        location = /index.html {
            add_header Cache-Control "no-store" always;
            add_header X-Robots-Tag "noindex, nofollow" always;
        }

        error_page 404 /_sitepass/expired.html;
        location /_sitepass/ {
            root /srv/sitepass/system;
            internal;
        }
    }

The SPA fallback is conditional on `Accept: text/html`. A missing image
returns 404 rather than a fragment of HTML, which keeps the browser
console readable and avoids masking real problems.

`no-cache` with ETag, rather than a long max-age: the usage loop is
re-upload followed by refresh. Serving a stale page after a re-upload
looks like a broken service. Preview traffic is negligible, so there is
nothing to gain from aggressive caching.

`X-Robots-Tag: noindex` on every response. Previews must not enter search
indexes.

### 10.3 TLS

Wildcard certificate for the preview domain, obtained via ACME DNS-01.
HTTP-01 cannot issue wildcards. The DNS provider credential is supplied to
the host at bootstrap and is never stored in the repository.

HSTS on the control domain with `includeSubDomains`. HSTS on the preview
domain without preload.

---

## 11. Security

### 11.1 Process

    NoNewPrivileges=yes
    ProtectSystem=strict
    ProtectHome=yes
    PrivateTmp=yes
    PrivateDevices=yes
    ProtectKernelTunables=yes
    ProtectControlGroups=yes
    RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
    RestrictNamespaces=yes
    LockPersonality=yes
    MemoryDenyWriteExecute=yes
    SystemCallFilter=@system-service
    SystemCallArchitectures=native
    ReadWritePaths=/srv/sitepass/builds /var/lib/sitepass
    MemoryMax=512M

### 11.2 Network

nftables, default deny inbound:

    table inet filter {
      chain input {
        type filter hook input priority 0; policy drop;
        ct state established,related accept
        ct state invalid drop
        iif lo accept
        ip protocol icmp icmp type echo-request limit rate 5/second accept
        ip6 nexthdr ipv6-icmp accept
        tcp dport 22 ct state new limit rate 6/minute accept
        tcp dport { 80, 443 } accept
      }
      chain forward { type filter hook forward priority 0; policy drop; }
      chain output  { type filter hook output priority 0; policy accept; }
    }

nftables rather than iptables: it is the native layer on Debian 13, and a
compatibility shim adds an unnecessary translation step to the one part of
the system that must be readable during an incident.

PostgreSQL listens on a unix socket only.

### 11.3 Abuse

Free anonymous hosting of arbitrary HTML on a domain with valid TLS is
attractive to phishing. Without controls the domain reaches browser
blocklists, at which point it is unusable.

Required in MVP:

- Preview content on a separate registrable domain from the control site
- `X-Robots-Tag: noindex, nofollow` on all preview responses
- An abuse report form on the control site, and an `abuse@` address
- An operator kill switch: revoke by token id or by subdomain, immediate
- Audit record per upload: token id, source IP, user agent, archive
  SHA-256, timestamp. Retained 30 days
- Rate limits per IP on token creation

Deliberately not in MVP: content scanning or classification. It is
expensive, unreliable, and the 30-minute lifetime already limits value to
an attacker.

### 11.4 Secrets

No secret is ever committed. The database password is generated on the
host during bootstrap. The ACME DNS credential is supplied at bootstrap.
Both live in `/etc/sitepass/sitepass.env`, mode `0600`, owned by root.

The repository contains `.env.example` with every variable documented and
no values.

---

## 12. Localisation

Languages: Russian (`ru`), Kazakh (`kk`), English (`en`). Default `en`.

- All user-visible strings live in `locales/<lang>.json`. No exceptions.
- Keys are namespaced by surface: `token.create.button`,
  `error.archive_too_large.message`.
- Selection order: explicit user choice in `localStorage`, then
  `Accept-Language`, then default.
- API `message` fields are localised from the same catalogues, negotiated
  from `Accept-Language`. `code` is never localised.
- `/llms.txt` is English only. It is read by agents, not by people.
- The nginx expiry page is a static file rendered per language at build
  time and selected by `Accept-Language`.
- Kazakh uses Cyrillic. The chosen typeface must cover
  ә ғ қ ң ө ұ ү һ і in all weights used.

CI fails the build if: any catalogue is missing a key present in another;
any catalogue has a key absent from `en`; any source file contains a
user-visible string literal outside the catalogues. The third check is a
lint rule over JSX text nodes and known attribute names.

---

## 13. Analytics

Events written to `events`:

| Event | Properties |
|---|---|
| `token_created` | `has_project_name`, `language` |
| `upload_accepted` | `size_bucket`, `file_count_bucket`, `format`, `revision`, `warning_codes` |
| `upload_rejected` | `error_code`, `size_bucket`, `format` |
| `token_expired` | `upload_count`, `had_build` |
| `token_revoked` | `by` (`user` or `operator`) |
| `preview_viewed` | `token_id`, `count` — aggregated hourly from access logs |

Derived metrics that the schema must support from day one:

- Share of tokens that receive at least one upload — the activation rate
- Distribution of uploads per token — settles whether repeat uploads
  matter and how much
- Share of tokens followed within two minutes by another token from the
  same IP — the signal for whether users are hitting a wall
- Rejection reasons by frequency — tells which part of the contract is
  unclear
- Time from token creation to first successful upload

Sizes are bucketed, not stored raw. No IP addresses in `events`; the audit
log for abuse is separate and has its own retention.

---

## 14. Self-hosting

The repository must be deployable by someone who is not the author.

### 14.1 Configuration

Everything environment-specific is a variable. Nothing about
`sitepass.tech` appears in source.

    SITEPASS_CONTROL_DOMAIN=sitepass.example
    SITEPASS_PREVIEW_DOMAIN=preview.example
    SITEPASS_BRAND_NAME=Sitepass
    SITEPASS_LISTEN=127.0.0.1:8080
    SITEPASS_BUILDS_DIR=/srv/sitepass/builds
    SITEPASS_DB_DSN=postgres:///sitepass?host=/var/run/postgresql
    SITEPASS_DEFAULT_LANGUAGE=en
    SITEPASS_TOKEN_TTL_SECONDS=1800
    SITEPASS_MAX_ARCHIVE_BYTES=104857600
    SITEPASS_DISK_HIGH_WATER_PERCENT=80
    SITEPASS_DISK_CRITICAL_PERCENT=90
    SITEPASS_ABUSE_CONTACT=abuse@sitepass.example
    SITEPASS_ACME_DNS_PROVIDER=
    SITEPASS_ACME_DNS_TOKEN=

The control and preview domains may be different registrable domains, or
the same hostname for shared-apex mode (`CONTROL == PREVIEW`, previews at
`<label>.<domain>`). Bootstrap refuses a split pair that shares a
registrable suffix without being equal.

### 14.2 Install

    git clone https://github.com/<owner>/sitepass.git /opt/sitepass
    cd /opt/sitepass
    sudo cp deploy/sitepass.env.example /etc/sitepass/sitepass.env
    sudo editor /etc/sitepass/sitepass.env
    sudo ./deploy/bootstrap.sh

`bootstrap.sh` is idempotent and is also the upgrade path. It:

1. Verifies Debian amd64, root, free space, and domain rules
2. Installs PostgreSQL, Caddy, and build tooling (Go, Node) when missing
3. Prepares `/srv/sitepass/builds` and system pages
4. Ensures the database role and database exist
5. Builds the Go binary and control UI from the checked-out tree
6. Renders systemd and Caddyfile templates from the environment file
7. Enables and restarts the services
8. Runs a health smoke check

Release-tarball install and nginx/nftables templates from earlier drafts
are superseded by this Caddy-based path.

Every step is announced before it runs and reports its outcome. A failure
names the step, the cause, and what to check.

### 14.3 Upgrade and rollback

    cd /opt/sitepass && git pull && sudo ./deploy/bootstrap.sh

Rollback is `git checkout <tag>` followed by the same script, because
`VERSION` is versioned with the source.

Migrations must be forward-compatible: additive only, no destructive
change in the same release as the code that stops using a column. A column
is dropped one release after the last code that reads it. Otherwise
rollback breaks the service.

### 14.4 Smoke test

`make verify` against a running instance: create a token, upload a fixture
archive, fetch the preview URL, assert the expected content, delete the
token, assert 404. Roughly thirty lines, run as the last bootstrap step
and available on demand.

---

## 15. Repository layout

    sitepass/
      cmd/sitepass/           entry point, wiring only
      internal/
        config/                environment parsing and validation
        token/                 generation, hashing, lookup
        intake/                request body to verified temp file
        archive/               format detection, unpacking, validation
        publish/               staging, atomic switch, removal
        lifecycle/             reaper, reconciler
        storage/               repositories, migrations runner
        httpapi/               handlers, envelope, error mapping
        i18n/                  catalogue loading and negotiation
        analytics/             event writing, log aggregation
        health/                disk and dependency checks
      web/                     control site source, builds to web/dist
      locales/                 ru.json kk.json en.json
      migrations/              NNNN_name.up.sql / .down.sql
      deploy/
        bootstrap.sh
        sitepass.env.example
        templates/             sitepass.service, nginx/*.conf, nftables.conf
      testdata/archives/       fixtures, including hostile ones
      docs/
      llms.txt
      VERSION
      Makefile

---

## 16. Test fixtures

`testdata/archives/` must contain at least these, each with an asserted
outcome. No change to `internal/archive` ships without a fixture.

| Fixture | Expected |
|---|---|
| `valid-spa.tar.gz` | Accepted |
| `valid-nested-dist.tar.gz` | Accepted, descends one level |
| `valid-single.html` | Accepted |
| `traversal.tar.gz` | `archive_unsafe_entry` |
| `absolute-path.zip` | `archive_unsafe_entry` |
| `symlink-escape.tar.gz` | `archive_unsafe_entry` |
| `hardlink.tar.gz` | `archive_unsafe_entry` |
| `device-node.tar.gz` | `archive_unsafe_entry` |
| `null-byte-name.tar.gz` | `archive_unsafe_entry` |
| `bomb-1000x.zip` | `archive_unpacked_too_large` |
| `many-files.tar.gz` | `archive_too_many_files` |
| `deep-nesting.tar.gz` | `archive_unsafe_entry` |
| `no-index.tar.gz` | `entrypoint_not_found` |
| `truncated.tar.gz` | `archive_malformed` |
| `case-collision.tar.gz` | Accepted on Linux; documents the difference |

Tests run on Linux. The case-collision fixture exists specifically because
this class of bug is invisible on a case-insensitive filesystem.

---

## 17. Acceptance criteria for MVP

1. A token can be created without an account and returns a working
   preview URL before any upload.
2. An upload of a valid static build is published and reachable over HTTPS
   within five seconds of the request completing.
3. A second upload with the same token replaces the content at the same
   URL, and a browser refresh shows the new content without a manual cache
   clear.
4. Every fixture in section 16 produces its expected outcome.
5. A build is unreachable and its files are absent from disk within 90
   seconds of expiry.
6. Killing the process mid-upload leaves no orphaned directory after the
   reconciler runs.
7. The interface is complete in all three languages, with no string
   literal outside the catalogues, verified by CI.
8. A fresh Debian 13 host reaches a passing smoke test using only
   `git clone` and `bootstrap.sh`.
9. Running `bootstrap.sh` a second time changes nothing and still passes.
10. `/llms.txt` documents every error code the implementation can emit,
    verified by test.
11. An SPA with client-side routing works on direct entry to a nested
    route and on refresh.
12. No secret appears anywhere in the repository history.
