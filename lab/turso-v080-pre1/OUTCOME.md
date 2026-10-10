# Turso 0.8.0-pre.1 Lab Outcome

## 2026-07-23 — Prerelease Forward-Compatibility Pass

`tursogo v0.8.0-pre.1` passed the complete isolated Go-driver suite. This is
forward-compatibility evidence only; the root package should stay on a stable
Turso release.

The failed-UPSERT regression left both rows and both unique indexes unchanged,
and `PRAGMA integrity_check` returned `ok`.

The large literal `IN (...)` regression produced:

```text
SEARCH records USING INDEX records_lookup_key (lookup_key=?)
```

It did not scan the table or covering index.

The tested engine boundaries remain unchanged from 0.7.0: experimental plain
`WITHOUT ROWID` works while MVCC writes reject it; MVCC rejects multiprocess
WAL; `BEGIN CONCURRENT` works on a reserved connection; native FTS remains
outside the Go/MVCC contract; same-process MVCC concurrency passes; sequential
child writers pass; overlapping plain writers require experimental
multiprocess WAL.

The optional CLI probes were not run because no matching binary path was
supplied.
