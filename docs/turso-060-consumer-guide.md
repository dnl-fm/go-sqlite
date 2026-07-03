# Turso 0.6.0 Consumer Guide

Turso 0.6.0 added real engine behavior, but consumers should not flatten it
into "Turso supports everything now." The details matter.

## What changed

`go-sqlite` now depends on `turso.tech/database/tursogo v0.6.0`. The Go API did
not materially change, but the embedded engine did. The `v0.6.1`,
`v0.7.0-pre.2`, `v0.7.0-pre.4`, `v0.7.0-pre.5`, `v0.7.0-pre.6`,
`v0.7.0-pre.10`, and `v0.7.0-pre.13` labs keep the same consumer guidance:
useful engine fixes, no new `go-sqlite` promise yet.

| Feature | Consumer rule |
|---|---|
| `WITHOUT ROWID` | Plain Turso can use it only with `?experimental=without_rowid`. `go-sqlite` requires normal rowid tables and rejects `WITHOUT ROWID` schemas. |
| MVCC writes | `database.Open` uses Turso MVCC by default. Use `ConcurrentTxRetry` / `BEGIN CONCURRENT` on one reserved connection for multi-statement concurrent writes. |
| Live CLI inspection | `database.Open` enables `experimental=multiprocess_wal` by default for local file databases. Use `tursodb --experimental-multiprocess-wal db ...` in the CLI. The `v0.7.0-pre.10` and `v0.7.0-pre.13` labs reconfirm this with matching CLIs; they did not introduce the capability. |
| Concurrent Go writer processes | Not a promise. The labs still see WAL file-locking with multiple Go child writers through `tursogo v0.6.0`, `v0.6.1`, `v0.7.0-pre.2`, `v0.7.0-pre.4`, `v0.7.0-pre.5`, `v0.7.0-pre.6`, `v0.7.0-pre.10`, and `v0.7.0-pre.13`. |

## Process layout

Organize write processes by database file. One process can own one file, and
many such processes can write in parallel because they do not share a WAL file.
That is the boring layout, which is exactly why it is the right one.

Do not run several independent Go writer processes against the same physical
database file and expect `experimental=multiprocess_wal` to make it safe. The
flag is useful for live inspection with the matching `tursodb` binary, and it
may coordinate readers and writers in Turso's engine path, but our Go-driver
labs still see WAL file-locking when several Go processes write the same file at
the same time.

Use this split:

| Shape | Status | Rule |
|---|---|---|
| One Go process, one DB file, many goroutines/connections | supported by lab evidence | Use MVCC and `ConcurrentTxRetry` for multi-statement writes. |
| Many Go processes, each with its own DB file | supported by isolation | Good process layout for per-tenant, per-customer, or per-worker files. |
| One Go process plus matching `tursodb` inspecting the same file | supported by pre6/pre10/pre13 optional labs | `database.Open` enables the app-side flag for local files; use `--experimental-multiprocess-wal` in the CLI. |
| Many Go processes writing the same DB file | not supported by this package | Route writes through one owner process, split the files, or add an app-level queue. |

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

For the day-to-day operator rule, read `docs/turso-operations.md`.

The lab evidence lives in `lab/turso-v060`, `lab/turso-v061`,
`lab/turso-v070-pre2`, `lab/turso-v070-pre4`, `lab/turso-v070-pre5`,
`lab/turso-v070-pre6`, `lab/turso-v070-pre10`, and `lab/turso-v070-pre13`. It
is intentionally not part of `pkg/` because it tests upstream behavior we may
or may not wrap.
