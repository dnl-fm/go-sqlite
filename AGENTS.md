# github.com/dnl-fm/go-sqlite

## Stack

- go
- sqlite

## Components

| Name | Path | Type |
|------|------|------|
| migrate | `cmd/migrate/` | cli |
| pkg | `pkg/` | lib |

## Commands

```bash
# Test
make test

# Exploratory Turso labs
make lab-test

# Native-memory probes per Turso version (slow; see lab/turso-memory/OUTCOME.md)
make lab-memory TURSO_VERSIONS="v0.7.2 v0.8.1"

# Build migrate CLI
make build
```

## Turso Consumer Notes

Apps using `github.com/dnl-fm/go-sqlite` should treat Turso features by mode,
not by marketing headline.

- `database.Open`, `DefaultConfig`, `ProductionConfig`, and `DevelopmentConfig`
  all use Turso MVCC. There is no supported old-WAL mode in this package.
- Turso MVCC still means normal rowid tables only. `database.Open`,
  `database.Exec`, and the migration runner enforce that requirement. Turso
  0.6.0, 0.6.1, 0.7.0-pre.2, 0.7.0-pre.4, 0.7.0-pre.5, 0.7.0-pre.6,
  0.7.0-pre.10, 0.7.0-pre.13, and final 0.7.0 through 0.7.2 can create
  experimental plain-engine `WITHOUT ROWID` tables with
  `?experimental=without_rowid`, but MVCC rejects writes to those tables.
- Turso 0.7.2 fixes MVCC sequence bookkeeping leaking into user-visible change
  counts. The 0.7.2 lab proves `AUTOINCREMENT` reports user-row changes only.
- Every pooled connection gets the configured PRAGMAs (connector-based since
  2026-10-07; before that only one of four did). Defaults include
  `mvcc_gc_threshold = 1024` and `mvcc_checkpoint_threshold = 262144`, which
  halve steady-state MVCC memory at Turso's throughput. Run long-lived services
  with `MALLOC_ARENA_MAX=2`. Never hold `Rows` or a `Tx` open: an open reader
  pins MVCC GC. Evidence: `lab/turso-memory/OUTCOME.md`.
- Multi-statement writes on MVCC paths should use `ConcurrentTxRetry` or
  `BEGIN CONCURRENT` on one reserved connection.
- Keep Turso SQL placeholders positional (`?`) unless a consuming app has
  explicit tests for named parameters on its exact driver path.
- Turso 0.7 makes MVCC and multiprocess WAL mutually exclusive. This package
  chooses MVCC and one owning process per file. `EnableMultiprocessWAL` and
  `WithMultiprocessWAL` remain for source compatibility, but enabling either is
  rejected when the database opens.
- Agents and CLI tools read a live database through the owning app's
  authenticated API or local admin socket. Prefer named inspection commands;
  normal tools never open or write the canonical file directly.
- Install `tursodb` on machines that inspect offline database artifacts. Use it
  instead of system `sqlite3`, and only against an owner-produced snapshot or a
  cleanly closed database. Never raw-copy a live Turso database as evidence.
- Organize services so one writer process owns one physical DB file. Many
  processes can write in parallel when each process has its own file. If several
  services need to mutate the same file, route writes through one owner process
  or an app-level queue.

The executable evidence lives in `lab/turso-v060`, `lab/turso-v061`,
`lab/turso-v070-pre2`, `lab/turso-v070-pre4`, `lab/turso-v070-pre5`,
`lab/turso-v070-pre6`, `lab/turso-v070-pre10`, `lab/turso-v070-pre13`,
`lab/turso-v070`, `lab/turso-v071`, and `lab/turso-v072`.
When bumping consuming apps, run their normal test gate and add an app-local
regression if they use Turso MVCC, `WITHOUT ROWID`, or live DB inspection.
