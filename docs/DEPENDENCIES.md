# Dependencies

Default answer is no. Each entry states what it does and what it replaces.

## Go

| Module | Why |
|--------|-----|
| `github.com/jackc/pgx/v5` | PostgreSQL driver without CGO |
| `github.com/klauspost/compress` | zstd reader for `.tar.zst` intake (stdlib has gzip/zip only) |
| `github.com/google/uuid` | Token row IDs |

No HTTP router framework, no ORM.

## Web (`web/`)

| Package | Why |
|---------|-----|
| `react` / `react-dom` | Single-screen control UI |
| `vite` / `@vitejs/plugin-react` | Build to `web/dist` |
| `@fontsource-variable/onest` | Self-hosted human typeface with Cyrillic (incl. Kazakh coverage in family) |
| `@fontsource-variable/jetbrains-mono` | Self-hosted machine typeface |
