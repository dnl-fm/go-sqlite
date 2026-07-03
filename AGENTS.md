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
  0.7.0-pre.10, and 0.7.0-pre.13 can create
  experimental plain-engine `WITHOUT ROWID` tables with
  `?experimental=without_rowid`, but MVCC rejects writes to those tables.
- Multi-statement writes on MVCC paths should use `ConcurrentTxRetry` or
  `BEGIN CONCURRENT` on one reserved connection.
- Keep Turso SQL placeholders positional (`?`) unless a consuming app has
  explicit tests for named parameters on its exact driver path.
- Install `tursodb` on machines that run or inspect database files. Use it
  instead of system `sqlite3`; it can read normal SQLite databases and is
  required for Turso-format databases. Servers running Turso-backed apps should
  have it available for incident inspection on the owning host.
- `database.Open` enables `experimental=multiprocess_wal` by default for local
  file databases. That is useful for live inspection: a Go app can keep a DB
  open and the matching `tursodb` CLI can read/write it with
  `--experimental-multiprocess-wal`.
- Do not read that as "many Go writer processes are safe now." The lab still
  sees WAL file-locking when multiple Go child processes write concurrently
  through `tursogo v0.6.0`, `v0.6.1`, `v0.7.0-pre.2`, `v0.7.0-pre.4`,
  `v0.7.0-pre.5`, `v0.7.0-pre.6`, `v0.7.0-pre.10`, and `v0.7.0-pre.13`.
- Organize services so one writer process owns one physical DB file. Many
  processes can write in parallel when each process has its own file. If several
  services need to mutate the same file, route writes through one owner process
  or an app-level queue; do not rely on `experimental=multiprocess_wal` for
  concurrent Go writer fleets.

The executable evidence lives in `lab/turso-v060`, `lab/turso-v061`,
`lab/turso-v070-pre2`, `lab/turso-v070-pre4`, `lab/turso-v070-pre5`,
`lab/turso-v070-pre6`, `lab/turso-v070-pre10`, and `lab/turso-v070-pre13`.
When bumping consuming apps, run their normal test gate and add an app-local
regression if they use Turso MVCC, `WITHOUT ROWID`, or live DB inspection.
