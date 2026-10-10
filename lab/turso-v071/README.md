# Turso 0.7.1 Lab

Turso 0.7.1 passes the existing 0.7.0 Go-driver boundaries and both regressions
that matter to this repository. This is executable adoption evidence for the
stable release, separate from the root module.

## Observed Behavior

The lab proves:

- a failed `UPSERT ... DO UPDATE` that collides with a second `UNIQUE`
  constraint leaves both rows and both unique indexes intact;
  `PRAGMA integrity_check` returns `ok`;
- a 1,000-value literal `IN (...)` query uses
  `SEARCH ... USING INDEX ... (lookup_key=?)`, not a table or covering-index
  scan;
- plain `WITHOUT ROWID` remains experimental, works with its flag, and still
  rejects writes under MVCC;
- MVCC rejects multiprocess WAL and supports `BEGIN CONCURRENT` on one reserved
  connection;
- native FTS remains gated, unavailable in the plain Go driver, and unsupported
  under MVCC;
- 32 same-process MVCC writers complete 1,600 autocommit inserts;
- sequential child processes work, overlapping plain-engine processes fail
  without multiprocess WAL, and overlapping processes pass with it.

Matching-version CLI probes for native FTS and live plain multiprocess WAL are
optional. They do not change the package's one-owner MVCC contract.

## Run

```bash
cd lab/turso-v071
GOWORK=off go test ./...
```

With a matching `tursodb 0.7.1` binary:

```bash
TURSO_V071_TURSODB_BIN=/path/to/tursodb GOWORK=off go test ./...
```
