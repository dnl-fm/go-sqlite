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

## Turso 0.6.0 Consumer Notes

Apps using `github.com/dnl-fm/go-sqlite` should treat Turso features by mode,
not by marketing headline.

- `WithTursoMVCC()` still means normal rowid tables only. Turso 0.6.0 can create
  experimental plain-engine `WITHOUT ROWID` tables with
  `?experimental=without_rowid`, but MVCC rejects writes to those tables.
- Multi-statement writes on MVCC paths should use `ConcurrentTxRetry` or
  `BEGIN CONCURRENT` on one reserved connection.
- Keep Turso SQL placeholders positional (`?`) unless a consuming app has
  explicit tests for named parameters on its exact driver path.
- `experimental=multiprocess_wal` is useful for live inspection: a Go app can
  keep a DB open and the matching `tursodb 0.6.0` CLI can read/write it with
  `--experimental-multiprocess-wal`.
- Do not read that as "many Go writer processes are safe now." The lab still
  sees WAL file-locking when multiple Go child processes write concurrently
  through `tursogo v0.6.0`.

The executable evidence lives in `lab/turso-v060`. When bumping consuming apps,
run their normal test gate and add an app-local regression if they use Turso
MVCC, `WITHOUT ROWID`, or live DB inspection.
