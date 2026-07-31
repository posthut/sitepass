# Sitepass — Code Contract

Binding for all code in this repository. Review rejects on violation.

---

## 1. Single responsibility

A module has one reason to change. State it in one sentence at the top of
the package. If the sentence needs "and", the package is two packages.

    // Package archive detects an artifact's format and unpacks it into a
    // staging directory, rejecting any entry that could escape it.

Concretely:

- `intake` moves bytes from a request to a verified temp file. It does not
  know what an archive is.
- `archive` unpacks and validates. It does not know about HTTP, tokens, or
  the database.
- `publish` performs the atomic switch. It does not know why.
- `httpapi` translates between HTTP and domain calls. It contains no
  business rules.

The test: a package that imports `net/http` and also writes to disk and
also queries the database is doing three jobs.

---

## 2. Naming

A name is read far more often than it is written. One reading should
establish what it is and why it exists.

    Good                            Bad
    ─────────────────────────────   ──────────────────────
    UnpackArchiveIntoStagingDir     Process
    rejectUnsafeEntry               check
    tokenExpiresAt                  exp
    diskHighWaterPercent            threshold
    ErrEntrypointNotFound           ErrNotFound
    publishAtomically               doPublish

Rules:

- No abbreviations except established ones: `id`, `url`, `db`, `http`,
  `tls`, `dsn`, `ip`.
- No `data`, `info`, `manager`, `helper`, `util`, `handler2`, `tmp`,
  `process`, `handle` as a whole name.
- Booleans read as assertions: `isExpired`, `hasBuild`, `acceptsUploads`.
- Functions with side effects are verbs; pure functions returning a value
  are nouns or `nounOf` forms.
- Durations and sizes carry units: `ttlSeconds`, `maxArchiveBytes`,
  `graceperiod` is wrong, `removalGracePeriod` is right.
- The same concept has the same name everywhere: it is `token_id` in SQL,
  `TokenID` in Go, `token_id` in JSON. No synonyms across layers.

Comments explain why. What the code does must be legible from the code
itself; a comment restating it is a naming failure being papered over.

---

## 3. File size

**300 lines is the limit.** Blank lines and comments count. Test files and
generated files are exempt.

The number is a detector, not a goal. A file crossing 300 lines has almost
always accumulated a second responsibility. The correct response is to
find that responsibility and extract it. The incorrect response is to move
150 lines into a file named `archive2.go`.

Splitting correctly:

    Wrong                          Right
    ──────────────────────────     ─────────────────────────────
    archive.go                     archive/detect.go
    archive_part2.go                 format detection
                                   archive/unpack.go
                                     entry iteration and extraction
                                   archive/validate.go
                                     entry safety rules
                                   archive/entrypoint.go
                                     locating index.html

Each resulting file is independently testable and independently
comprehensible. If the two halves need to reach into each other's
internals, the split was wrong; find a different seam.

CI enforces the limit. There is no override.

---

## 4. Structure

    cmd/sitepass/       wiring only: read config, construct, run, shut down
    internal/<domain>/   one responsibility per package, per section 1
    web/                 control site, builds to web/dist
    locales/             translation catalogues
    migrations/          numbered, forward and reverse
    deploy/              bootstrap and templates
    testdata/            fixtures

Dependencies point inward. `httpapi` may import `archive`; `archive` may
never import `httpapi`. No import cycles, no shared `common` package —
a package named `common` is an admission that the boundaries were not
found.

Constructors take dependencies as interfaces defined by the consumer, not
the producer. This keeps packages testable without a database or a
filesystem.

`cmd/sitepass/main.go` contains no logic. It reads configuration,
constructs components, starts them, and handles shutdown. It stays under
150 lines.

---

## 5. Errors

Every failure mode reachable by a client has a registered code. Codes live
in one place:

    // internal/httpapi/errorcodes.go
    const (
        CodeTokenNotFound     ErrorCode = "token_not_found"
        CodeArchiveTooLarge   ErrorCode = "archive_too_large"
        ...
    )

Rules:

- Errors are wrapped with context on the way up: `fmt.Errorf("unpack
  entry %q: %w", name, err)`. Never discarded, never logged and returned
  both.
- A sentinel error is defined for every condition the caller must
  distinguish. Callers use `errors.Is` and `errors.As`, never string
  matching.
- Internal error text never reaches the client. The API returns the
  registered code, a localised message, and a correlation id. The detail
  goes to the log against that id.
- The mapping from domain error to HTTP status and code lives in exactly
  one function.
- A test asserts that every registered code appears in `llms.txt` and that
  `llms.txt` contains no unregistered codes.

Panics are for programmer errors only, never for input. The HTTP server
has a recovery middleware that logs with a correlation id and returns
`internal_error`.

---

## 6. Logging

Structured, `log/slog`, JSON to stdout, collected by journald.

Levels:

    error   the service failed to do its job; needs a human
    warn    degraded but functioning: disk high-water, dependency retry
    info    lifecycle: start, stop, migration applied, token purged
    debug   development only, off in production

Every log line for a request carries `correlation_id`. Every line touching
a token carries `token_id` — never the token itself, and never the hash.

**Never logged:** plaintext tokens, database credentials, ACME
credentials, request bodies, archive contents, full file listings.

Logging is not analytics. Product measurement goes to the `events` table
through the `analytics` package. Logs are for operating the service; if a
metric is being computed by grepping logs, it belongs in `events`.

---

## 7. Configuration

All configuration is environment variables, parsed once at startup into a
typed struct, validated immediately. Invalid configuration is a startup
failure with a message naming the variable and the problem — never a
default silently substituted.

    Hard rules
    ──────────────────────────────────────────────────────────
    No hostname, domain, path, port or limit is written in source.
    No os.Getenv outside internal/config.
    Every variable appears in deploy/sitepass.env.example with a comment.
    Limits that vary by plan are read from the database, not the env.

The example file and the config struct are compared by a test. A variable
added to one and not the other fails the build.

---

## 8. Localisation

- No user-visible string literal exists in source. Not in components, not
  in error construction, not in the expiry page template.
- Catalogues are `locales/{en,ru,kk}.json`, flat keys namespaced by
  surface: `token.create.button`, `error.archive_too_large.message`.
- `en` is the reference. A key must exist in `en` before it exists
  anywhere.
- CI fails if any catalogue is missing a key present in `en`, if any
  catalogue has a key absent from `en`, or if a lint rule finds a literal
  string in a rendered position.
- Interpolation uses named placeholders: `{size}`, `{limit}`. Never
  positional, never string concatenation — word order differs by language.
- Pluralisation uses the platform's plural rules. Russian and Kazakh do
  not share English's two-form model, and `count === 1 ? a : b` is a bug.
- `llms.txt` is English only and is exempt.

---

## 9. Database

- Every change is a numbered migration pair, `NNNN_name.up.sql` and
  `.down.sql`. No schema change outside migrations, ever.
- Migrations are additive within a release. A column is dropped no earlier
  than one release after the last code that reads it. This is what makes
  rollback safe.
- Parameterised queries only. String-built SQL is a review rejection.
- Every query has a context with a timeout.
- Transactions are as short as possible and never span an HTTP call, a
  filesystem walk, or an unpack.
- The `pgx` driver, no CGO.

---

## 10. Filesystem

The build directory is the only writable path outside `/var/lib/sitepass`.

- Every path derived from user input is validated before use. Validation
  lives in `archive`, and no other package constructs a path from
  untrusted input.
- Writes go to a staging directory and become visible by `rename`. No
  partially written tree is ever reachable.
- Removal is by token id, never by a path assembled from a label at the
  call site.
- Temporary files are created with `os.CreateTemp` in a directory the
  process owns, and are removed by a deferred call on every path,
  including error paths.
- The reconciler treats the database as the source of truth and the
  filesystem as a cache. Never the reverse.

---

## 11. Security-critical code

`internal/archive` is designated security-critical. Rules that apply only
here:

- No change ships without a corresponding fixture in `testdata/archives/`.
- Every rejection branch has a test asserting the specific code returned.
- Reading is bounded by `io.LimitReader` at every level. A size declared
  in an archive header is treated as a hint, never as an allocation size.
- Allowlist, not denylist: regular files and directories are permitted,
  everything else is refused. A new entry type appearing in a format
  update fails closed.
- Path validation is duplicated deliberately — per entry during
  extraction, and once over the resulting tree with symlinks resolved. The
  redundancy is intentional and must not be removed as an optimisation.

---

## 12. Testing

    Unit          every package; no database, no network, no real filesystem
                  except through a temp dir
    Fixture       internal/archive against testdata/archives, one test per
                  fixture with an asserted outcome
    Integration   API against a real PostgreSQL, on Linux
    Smoke         make verify, end to end against a running instance

Tests run on Linux. Development happens on Linux. The case-sensitivity of
the filesystem is part of the contract, and a test suite that has only run
on a case-insensitive filesystem has not been run.

Table-driven tests for anything with more than two cases. Test names state
the case and the expectation:
`TestUnpack_SymlinkEscapingRoot_IsRejected`.

No test asserts on a localised message. Assert on codes.

---

## 13. Frontend

- Components are functions with one responsibility, in one file, under the
  same 300-line limit.
- No raw colour, spacing, radius or duration values. Only the custom
  properties named in the design contract.
- State lives at the lowest level that needs it. No global store for a
  single-screen application.
- No `localStorage` except for the language choice and the theme override.
- The countdown is computed from `expires_at` returned by the server, not
  from a client-side timer started at page load. Clock drift and
  backgrounded tabs both break the naive version.
- Every network call has a loading state, an error state, and an empty
  state, and all three are designed before the happy path is wired.
- No dependency is added without a note in `docs/DEPENDENCIES.md` stating
  what it does and what it replaces.

---

## 14. Dependencies

Default answer is no.

Add one when: it solves a problem that is genuinely hard to get right
(cryptography, compression formats, database drivers), it is actively
maintained, and its licence permits redistribution.

Do not add one for: string manipulation, date formatting, HTTP routing
beyond a simple multiplexer, configuration parsing, or anything under
roughly 200 lines to implement.

`CGO_ENABLED=0`. The binary is statically linked and runs on a host with
no toolchain.

---

## 15. Analytics

Product measurement is a first-class concern, not instrumentation added
later.

- Events are emitted through `internal/analytics`, never by writing to the
  table directly.
- Every event type is declared in one file with its property schema.
- Emission never blocks the request path and never fails it. A failed
  event write is logged at `warn` and dropped.
- Properties are bucketed, not raw. Sizes become buckets; counts become
  buckets.
- No IP address, user agent, token plaintext or file name enters `events`.
  The abuse audit log is a separate store with its own retention.

---

## 16. Documentation

The documents in `docs/` are part of the code, not commentary on it.

- A change to behaviour updates `SPECIFICATION.md` in the same commit.
- A change to a visual value updates `DESIGN_CONTRACT.md` in the same
  commit.
- A change to an error code updates `llms.txt` in the same commit, and a
  test enforces it.
- A new environment variable updates `sitepass.env.example` in the same
  commit, and a test enforces it.
- An architectural decision that reverses one recorded in
  `ARCHITECTURE.md` adds a new entry to the decision log with the reason.
  The old entry is not deleted.

A pull request that changes behaviour without touching documentation is
incomplete.

---

## 17. Commits and releases

    feat(archive): reject hardlink entries
    fix(publish): remove staging dir on switch failure
    docs(spec): record capacity thresholds

Conventional commits, imperative mood, scope is the package.

Releases are tags, `vMAJOR.MINOR.PATCH`. The tag builds the release
artifact. `VERSION` is committed and read by `bootstrap.sh`, so checking
out an older tag and re-running bootstrap is a complete rollback.

---

## 18. Review checklist

    [ ] Package responsibility still fits in one sentence without "and"
    [ ] No file over 300 lines
    [ ] Names readable once, units on quantities
    [ ] No user-visible string outside locales/
    [ ] No literal colour, spacing or duration in a component
    [ ] No configuration value in source
    [ ] Every new failure mode has a registered code, in llms.txt
    [ ] Errors wrapped with context; no string matching on errors
    [ ] No secret, token or body content in any log line
    [ ] Changes to internal/archive ship with a fixture
    [ ] Migration is additive and reversible
    [ ] Documentation updated in the same commit
    [ ] Tests pass on Linux
