# Turso Consumer Guide

Turso features are mode-specific. Consumers should not flatten a release
headline into a `go-sqlite` promise.

## What changed

`go-sqlite` depends on `turso.tech/database/tursogo v0.7.2`. The stable 0.7
labs extend the earlier 0.6 and prerelease evidence. The load-bearing boundary
is unchanged: MVCC and multiprocess WAL are mutually exclusive. Version 0.7.2
also fixes MVCC sequence bookkeeping leaking into user-visible change counts.

| Feature | Consumer rule |
|---|---|
| `WITHOUT ROWID` | Plain Turso can use it only with `?experimental=without_rowid`. `go-sqlite` requires normal rowid tables and rejects `WITHOUT ROWID` schemas. |
| MVCC writes | `database.Open` uses Turso MVCC by default. Use `ConcurrentTxRetry` / `BEGIN CONCURRENT` on one reserved connection for multi-statement concurrent writes. |
| MVCC change counts | `AUTOINCREMENT` sequence maintenance is internal. The 0.7.2 lab proves `RowsAffected`, `changes()`, and `total_changes()` count user rows only. |
| Live CLI inspection | Read through the owning app's authenticated API or local admin socket. MVCC and multiprocess WAL cannot share the live file. |
| Offline inspection | Use `tursodb` on an owner-produced snapshot or a cleanly closed database. Never raw-copy a live Turso file. |
| Concurrent Go writer processes | Not a package mode. One process owns each `go-sqlite` file; pooled connections provide same-process concurrency. |

## Process layout

Organize write processes by database file. One process can own one file, and
many such processes can write in parallel because they do not share a WAL file.
That is the boring layout, which is exactly why it is the right one.

Do not run several independent processes against the same physical database
file. Final Turso 0.7 rejects multiprocess WAL together with MVCC, and this
package keeps MVCC.

Use this split:

| Shape | Status | Rule |
|---|---|---|
| One Go process, one DB file, many goroutines/connections | supported by lab evidence | Use MVCC and `ConcurrentTxRetry` for multi-statement writes. |
| Many Go processes, each with its own DB file | supported by isolation | Good process layout for per-tenant, per-customer, or per-worker files. |
| Agent or CLI reading through the owning app | supported consumer pattern | Prefer named authenticated reads. |
| `tursodb` inspecting an owner-produced snapshot or closed DB | supported offline path | Use `tursodb`, not system `sqlite3`. |
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
   databases and is required for Turso-format databases. Use it against an
   owner-produced snapshot or a cleanly closed database.
7. If the app needs live production inspection, implement named authenticated
   reads or a guarded local admin socket and test that operator path.
8. If the app promises snapshots, produce them through the owning process and
   test row counts before relying on them for incidents.

For the day-to-day operator rule, read `docs/turso-operations.md`.

The lab evidence lives in `lab/turso-v060`, `lab/turso-v061`,
`lab/turso-v070-pre2`, `lab/turso-v070-pre4`, `lab/turso-v070-pre5`,
`lab/turso-v070-pre6`, `lab/turso-v070-pre10`, `lab/turso-v070-pre13`,
`lab/turso-v070`, `lab/turso-v071`, and `lab/turso-v072`. It is intentionally
not part of `pkg/` because it tests upstream behavior we may or may not wrap.
