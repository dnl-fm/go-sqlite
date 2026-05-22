# Turso 0.6.0 Consumer Guide

Turso 0.6.0 added real engine behavior, but consumers should not flatten it
into "Turso supports everything now." The details matter.

## What changed

`go-sqlite` now depends on `turso.tech/database/tursogo v0.6.0`. The Go API did
not materially change, but the embedded engine did. The `v0.6.1` and
`v0.7.0-pre.2` labs keep the same consumer guidance: useful engine fixes, no new
`go-sqlite` promise yet.

| Feature | Consumer rule |
|---|---|
| `WITHOUT ROWID` | Plain Turso can use it only with `?experimental=without_rowid`. `go-sqlite` requires normal rowid tables and rejects `WITHOUT ROWID` schemas. |
| MVCC writes | `database.Open` uses Turso MVCC by default. Use `ConcurrentTxRetry` / `BEGIN CONCURRENT` on one reserved connection for multi-statement concurrent writes. |
| Live CLI inspection | Use `db?experimental=multiprocess_wal` in the Go app and `tursodb --experimental-multiprocess-wal db ...` in the CLI. |
| Concurrent Go writer processes | Not a promise. The labs still see WAL file-locking with multiple Go child writers through `tursogo v0.6.0`, `v0.6.1`, and `v0.7.0-pre.2`. |

## Upgrade checklist for apps

1. Bump `github.com/dnl-fm/go-sqlite` to the release that contains this guide.
2. Run the app's normal Go test gate with `GOWORK=off` unless the repo documents
   a workspace-specific flow.
3. Search migrations for `WITHOUT ROWID`.
4. Keep `WITHOUT ROWID` out of the schema; this package opens databases with Turso MVCC and validates the rowid requirement.
5. Install `tursodb` on machines that run or inspect database files:
   ```bash
   curl --proto '=https' --tlsv1.2 -LsSf \
     https://github.com/tursodatabase/turso/releases/latest/download/turso_cli-installer.sh | sh
   source "$HOME/.turso/env"
   ```
6. Use `tursodb` instead of system `sqlite3`. It can read normal SQLite
   databases and is required for Turso-format databases. Servers running
   Turso-backed apps should have it available for incident inspection on the
   owning host.
7. If the app needs live production inspection, test the exact operator path
   with the matching `tursodb` release and `--experimental-multiprocess-wal`.

The lab evidence lives in `lab/turso-v060`, `lab/turso-v061`, and
`lab/turso-v070-pre2`. It is intentionally not part of `pkg/` because it tests
upstream behavior we may or may not wrap.
