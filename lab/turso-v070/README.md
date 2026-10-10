# Turso 0.7.0 Lab

Turso 0.7.0 changes the old prerelease answer in one important place:
`experimental=multiprocess_wal` is now real for overlapping plain-engine child
writers, but it is explicitly not compatible with MVCC.

The package tests under `pkg/` prove the supported `go-sqlite` contract. This
lab proves upstream behavior we are still deciding how to expose: experimental
`WITHOUT ROWID` support, native FTS, and multi-process access.

## What We Know

`WITHOUT ROWID` is no longer a simple "no." It is a "yes, but only for the
plain engine and only behind an experimental flag."

| mode | DSN | result |
|---|---|---|
| plain Turso | `db.sqlite` | rejects `WITHOUT ROWID` |
| plain Turso | `db.sqlite?experimental=without_rowid` | creates, inserts, reads |
| MVCC Turso | `db.sqlite?experimental=without_rowid` + `pragma journal_mode='mvcc'` | creates table, rejects write |

That last row matters for this repo. `WithTursoMVCC()` is still right to steer
users away from `WITHOUT ROWID` tables.

Native FTS is also not ready for the Go/MVCC package contract.

| mode | DSN / command | result |
|---|---|---|
| Go driver, no flag | `db.sqlite` | rejects `USING fts` as experimental index method |
| Go driver, plain Turso | `db.sqlite?experimental=index_method` | rejects `USING fts` with `unknown module name 'fts'` |
| Go driver, MVCC Turso | `db.sqlite?experimental=index_method` + `pragma journal_mode='mvcc'` | rejects custom index modules in MVCC |
| CLI, plain Turso | `tursodb --experimental-index-method db.sqlite` | can create/query native FTS |

That last CLI row is useful for sidecar/search-projection labs, not for
`go-sqlite`'s canonical MVCC path.

Multi-process access is also worth testing outside unit tests. A single process
can lie to you by accident. This lab forks child test processes and has them
open the same database file through the Turso driver.

Same-process concurrent writes are not the problem Turso MVCC has left open.
With one Go process, `journal_mode='mvcc'`, 32 pooled connections, and 32
goroutines inserting 1,600 total rows, autocommit writes pass without busy
errors. A reserved connection also completes `BEGIN CONCURRENT`, inserts one
row, commits it, and reads it back.

Sequential child processes can write the same database file without extra flags.
Overlapping child writers still fail without the experimental WAL path:

```text
db.sqlite?experimental=multiprocess_wal
```

Through `tursogo v0.7.0` on this Linux host, that flag lets 4 overlapping child
processes write 25 rows each and the lab verifies all 100 expected rows. The
same flag combined with `pragma journal_mode='mvcc'` is rejected.

The exact release CLI story has its own optional probe. With a matching
`tursodb 0.7.0` binary, the lab checks the plain native-FTS CLI path and a
plain multiprocess-WAL CLI probe. Those probes are about Turso engine behavior,
not a `go-sqlite` MVCC promise.

## Running It

```bash
cd lab/turso-v070
GOWORK=off go test ./...
```

The CLI probe is optional because it needs the release `tursodb` binary:

```bash
TURSO_V070_TURSODB_BIN=/path/to/tursodb GOWORK=off go test ./...
```

## Next Questions

- Should `go-sqlite` expose a plain Turso experimental config for
  `WITHOUT ROWID`, separate from `WithTursoMVCC()`?
- Should plain multi-process WAL get a separate product-level helper, or stay
  documented as a Turso engine behavior outside `go-sqlite` MVCC?
- Should native FTS get a product-level helper once the Go driver and MVCC path
  support custom index modules?
- Should the root README replace the blanket "Turso does not support
  WITHOUT ROWID" wording with the mode-specific rule above?
