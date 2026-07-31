# Sitepass — Architecture

Status: draft for implementation
Target platform: Debian 13 (amd64)
Audience: maintainers and self-hosters

---

## 1. What the service does

An agent uploads a built static site. The service publishes it on a
dedicated subdomain for a limited time and returns a URL.

The service does not build anything, does not install dependencies, and
does not execute customer code. It accepts an artifact and serves it.

That boundary is the core design decision. Everything below follows from it.

---

## 2. Components

    ┌──────────────────────────────────────────────────────────┐
    │ prod-VM (Debian 13)                                      │
    │                                                          │
    │  nginx ──────────────────────────────────────┐           │
    │    ├─ control vhost   sitepass.tech         │           │
    │    │    ├─ /            → static SPA files   │           │
    │    │    ├─ /api/*       → proxy to sitepass │           │
    │    │    └─ /llms.txt    → static file        │           │
    │    │                                          │          │
    │    └─ preview vhost   *.sitepass-app.tech   │           │
    │         └─ root /srv/sitepass/builds/$id/current        │
    │                                               │          │
    │  sitepass (Go binary, systemd) ◄─────────────┘          │
    │    ├─ HTTP API on 127.0.0.1:8080                         │
    │    ├─ archive intake and unpacking                       │
    │    ├─ publisher (atomic symlink switch)                  │
    │    ├─ reaper (TTL garbage collection)                    │
    │    ├─ reconciler (disk/DB drift repair)                  │
    │    └─ log ingester (preview view counts)                 │
    │                                                          │
    │  PostgreSQL 17 (native, unix socket only)                │
    │                                                          │
    │  /srv/sitepass/builds  (separate mount, nodev nosuid noexec) │
    └──────────────────────────────────────────────────────────┘

Nothing else runs on the production host. No Docker, no Node, no Go
toolchain, no package manager for customer dependencies.

### 2.1 Component responsibilities

| Component | Responsibility |
|---|---|
| nginx (control) | TLS, static frontend, reverse proxy to the API |
| nginx (preview) | Serving customer files, SPA fallback, cache headers |
| sitepass | Tokens, intake, unpacking, publishing, lifecycle, analytics |
| PostgreSQL | Tokens, builds, plans, events, rate-limit buckets |
| Filesystem | Published content, isolated on its own mount with quota |

The Go process is the only writer to both the database and the build
directory. nginx has read-only access to the build directory and no
database access at all.

---

## 3. Request flows

### 3.1 Token creation

    browser ──POST /api/v1/tokens──► nginx ──► sitepass
                                                  │
                                                  ├─ check IP rate limit
                                                  ├─ check disk headroom
                                                  ├─ generate 32 random bytes
                                                  ├─ derive subdomain label
                                                  ├─ store SHA-256 of token
                                                  └─ return token + URL + TTL

The plaintext token is returned once and never stored. The database holds
only its SHA-256 and a short non-secret prefix used for support and logs.

The URL is issued immediately, before any upload. The user can copy it,
open it, and see a placeholder page that says the preview is waiting for
content. This matters: the human hands the token to the agent and wants
somewhere to watch.

### 3.2 Upload

    agent ──POST /api/v1/upload──► nginx ──► sitepass
      Authorization: Bearer <token>            │
      multipart archive                        │
                                               ├─ resolve token, check expiry
                                               ├─ acquire row lock on token
                                               ├─ stream body to temp file
                                               ├─ verify size limits
                                               ├─ unpack into staging dir
                                               │     rejecting unsafe entries
                                               ├─ locate index.html
                                               ├─ collect warnings
                                               ├─ atomic publish
                                               ├─ schedule old revision removal
                                               └─ return URL + warnings

Streaming matters. The request body is written to a temp file with a hard
byte ceiling and never buffered in memory. A 100 MB limit must not become
100 MB of RSS per concurrent upload.

### 3.3 Publishing (atomic)

    /srv/sitepass/builds/<token_id>/
        rev-1/          previous revision
        rev-2/          newly unpacked
        current -> rev-2

Switch procedure:

    ln -sfn rev-2 current.tmp
    mv -Tf current.tmp current      # rename(2), atomic

`mv -T` on a symlink is a single `rename()` syscall, so no request ever
observes a missing or half-written root. The previous revision is deleted
after a 60 second grace period so that in-flight nginx responses are not
truncated.

### 3.4 Preview request

    browser ──► nginx preview vhost
                  ├─ extract subdomain label
                  ├─ root = /srv/sitepass/builds/<label>/current
                  ├─ try_files $uri $uri/ $uri.html <html fallback>
                  └─ Cache-Control: no-cache, ETag

If the directory does not exist, nginx serves the branded "expired or not
found" page. The Go process is not involved in serving previews at all.

### 3.5 Expiry

The reaper runs every 60 seconds:

    SELECT id FROM tokens
     WHERE expires_at < now() AND purged_at IS NULL
     LIMIT 100;

For each: remove the directory tree, set `purged_at`, emit a
`token_expired` event.

The reconciler runs at startup and hourly: it lists the build directory
and removes any directory with no live token row. This is the safety net
for crashes between filesystem and database operations. The filesystem is
never the source of truth; the database is.

---

## 4. Data flow for analytics

Preview traffic is measured by tailing the nginx access log for the
preview vhost and aggregating per subdomain label. No JavaScript is
injected into customer content and no tracking pixel is added.

This is deliberate. Injecting a script into an artifact the customer
handed us would modify their work without asking, and it would be the one
place where the service touches what it publishes. The access log already
contains everything needed.

---

## 5. Deployment topology

    dev-VM (Debian 13)                prod-VM (Debian 13)
      SSH development                   runtime only
      Go + Node toolchain               no toolchain
      full stack running                nginx + sitepass + postgres
      dev.sitepass.tech                sitepass.tech
      *.dev-app.sitepass-app.tech      *.sitepass-app.tech
            │
            │ git push --tags
            ▼
      GitHub (public repository)
        source + release artifact + SHA-256
            │
            │ git clone && ./deploy/bootstrap.sh
            ▼
      prod-VM

Both machines run the same distribution and the same major versions of
PostgreSQL and nginx. Parity is not maintained through configuration; it
exists because the environments are the same.

Production never compiles. It downloads a verified release artifact.

---

## 6. Decision log

Each entry records the decision, the reason, and what would reverse it.

### D-1. Static artifacts only, no runtime execution

Serving files requires no isolation of executing code, no container
orchestration, no CPU or memory limits per customer, and no egress
filtering. Publishing takes seconds rather than tens of seconds, which
matters because the usage loop is edit-rebuild-look.

Reverses if: a meaningful share of users cannot produce a static build.
The successor design is a self-contained bundle contract (esbuild/ncc
output), not server-side dependency installation.

### D-2. No dependency installation on the server

Frameworks exist at build time. After a production build, React, Vue and
Tailwind are compiled into the emitted JavaScript and CSS. The server
needs none of them.

Installing dependencies would mean minutes of latency, gigabytes of cache,
outbound network access from a host that ingests untrusted input, and
execution of arbitrary npm lifecycle scripts. The agent already has a
toolchain; it builds on its own machine.

Consequence for positioning: the service supports every framework, because
it has no knowledge of any framework. The contract is "a directory with
index.html", not a compatibility list.

### D-3. Subdomain per build, on a separate domain

Path-based hosting breaks assets referenced from the root (`/assets/app.js`),
which is what every default build configuration emits. Working around it
would require rewriting customer HTML and JavaScript, or forcing the agent
to change its build configuration — doing the customer's job.

A subdomain gives each build its own origin, so the browser isolates
storage, cookies and service workers between builds at no cost.

The preview domain is registered separately from the control domain so
that customer JavaScript cannot set cookies on the control domain.

Both domains should be submitted to the Public Suffix List.

### D-4. PostgreSQL, installed natively

The dataset is small and the query pattern is simple, so SQLite would
serve. PostgreSQL is chosen because analytics aggregation and eventual
multi-node operation both become painful otherwise, and the migration
cost later exceeds the setup cost now.

Native rather than containerised: a stateful singleton gains nothing from
a container on a single host, and backups, tuning and major upgrades are
all simpler. If a container runtime is ever added for customer workloads,
the database must not share a daemon with untrusted code.

### D-5. Go binary under systemd, not a container

One statically linked binary, one unit file, one configuration file. The
unit applies `ProtectSystem=strict`, `PrivateTmp`, `NoNewPrivileges` and a
syscall filter, which gives most of what a container would provide without
a daemon in the trust path.

### D-6. Token TTL measured from creation, not from last upload

A sliding window would let a client keep a preview alive indefinitely by
uploading periodically — free permanent hosting. A fixed window makes the
limit self-evident to the user and removes the need for a separate
absolute cap.

Re-uploads within the window are unlimited. Each replaces the previous
build. No history is kept.

### D-7. Rejections are limited to what prevents publication

The service rejects an upload when it cannot publish safely or at all:
malformed archive, unsafe entries, limits exceeded, no index.html.

Everything else is reported as a non-blocking warning. Diagnosing why a
customer's build is wrong is the agent's job. The service states facts and
does not give build advice.

The contract is communicated up front through the agent instruction block
and `/llms.txt`, rather than discovered through error messages.

### D-8. Development on Linux over SSH

Unpacking untrusted archives is the highest-risk code in the project. It
must be developed and tested on a case-sensitive filesystem with the same
kernel and the same init system as production. Developing on a
case-insensitive filesystem hides an entire class of bugs until deployment.

### D-9. Analytics from access logs, not injected scripts

See section 4.

---

## 7. Scaling path

The current design targets one host. The order in which pieces move:

1. **Content to object storage or a dedicated file server.** The build
   directory is the first thing to outgrow a single disk. nginx gains an
   upstream instead of a local root.
2. **Multiple API instances.** The Go process is stateless apart from the
   filesystem; once content is remote, instances scale horizontally behind
   a load balancer. The database already holds all state.
3. **Separate reaper.** Garbage collection becomes a leader-elected job or
   a separate service once more than one instance runs.
4. **Read replica for analytics.** Only if event volume justifies it.

None of this is built now. It is listed so that the schema and module
boundaries do not foreclose it.

---

## 8. Trust boundaries

| Boundary | Untrusted input | Control |
|---|---|---|
| Upload endpoint | Archive bytes | Size ceiling, streaming, no in-memory buffering |
| Unpacker | Entry paths, types, sizes | Path validation, type allowlist, ratio and count limits |
| Filesystem | Written content | Dedicated mount: nodev, nosuid, noexec, quota |
| Preview serving | Customer HTML/JS | Separate origin, separate domain, no cookies shared |
| Public internet | Everything | nftables default deny, rate limits, disk headroom checks |

The unpacker is the security-critical component. It gets its own module,
its own test fixture corpus, and a rule that no change ships without a
corresponding fixture.

---

## 9. What this architecture deliberately does not include

- Building, compiling, or transpiling customer projects
- Installing customer dependencies
- Executing customer server code
- Persistent hosting
- Custom domains
- Any customer data beyond the uploaded artifact and its metadata

These are not backlog items. They are the boundary that keeps the service
cheap enough to be free.
