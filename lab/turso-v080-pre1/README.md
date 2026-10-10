# Turso 0.8.0-pre.1 Lab

Turso 0.8.0-pre.1 is a prerelease. This lab is forward-compatibility evidence,
not a recommendation to move the root dependency off the stable release.

## Observed Behavior

The prerelease passes both 0.7.1 regressions:

- a failed `UPSERT ... DO UPDATE` against a second `UNIQUE` constraint leaves
  the table and both unique indexes intact; `PRAGMA integrity_check` returns
  `ok`;
- a 1,000-value literal `IN (...)` query selects indexed seeks through
  `SEARCH ... USING INDEX ... (lookup_key=?)`, not a table or covering-index
  scan.

It also preserves the tested 0.7.0 boundaries: plain and MVCC `WITHOUT ROWID`,
MVCC versus multiprocess WAL, reserved-connection `BEGIN CONCURRENT`, native
FTS through the Go driver and MVCC, same-process MVCC write concurrency, and
sequential versus overlapping child processes.

Matching-version CLI probes are optional and cover plain-engine behavior only.

## Run

```bash
cd lab/turso-v080-pre1
GOWORK=off go test ./...
```

With a matching `tursodb 0.8.0-pre.1` binary:

```bash
TURSO_V080_PRE1_TURSODB_BIN=/path/to/tursodb GOWORK=off go test ./...
```
