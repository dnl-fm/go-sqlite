# Turso 0.6.0 Lab

Turso 0.6.0 changed the shape of the engine faster than the public Go API
changed. That is exactly when a lab earns its rent.

The package tests under `pkg/` prove the supported `go-sqlite` contract. This
lab proves upstream behavior we are still deciding how to expose: experimental
`WITHOUT ROWID` support and multi-process access.

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

Multi-process access is also worth testing outside unit tests. A single process
can lie to you by accident. This lab forks child test processes and has them
open the same database file through the Turso driver.

The current result is more interesting than the release note. Sequential child
processes can write the same database file without extra flags. Turso documents
an experimental WAL path for opening a live database from another process:

```text
db.sqlite?experimental=multiprocess_wal
```

Through `tursogo v0.6.0` on this Linux host, concurrent child processes still
hit WAL file locking even with that flag. That may mean the Go binding path,
our DSN shape, or our test shape is still missing something. The lab keeps the
failure as evidence instead of pretending the release note settled it.

The exact release CLI story does work. With a Go app holding the database open
using `experimental=multiprocess_wal`, the `tursodb 0.6.0` shell can read and
write the same file using `--experimental-multiprocess-wal`.

## Running It

```bash
cd lab/turso-v060
GOWORK=off go test ./...
```

The CLI probe is optional because it needs the release `tursodb` binary:

```bash
TURSO_V060_TURSODB_BIN=/path/to/tursodb GOWORK=off go test ./...
```

## Next Questions

- Should `go-sqlite` expose a plain Turso experimental config for
  `WITHOUT ROWID`, separate from `WithTursoMVCC()`?
- Should multi-process WAL get a product-level helper, or stay documented as a
  Turso engine behavior until we know the operational edges?
- Should the root README replace the blanket "Turso does not support
  WITHOUT ROWID" wording with the mode-specific rule above?
