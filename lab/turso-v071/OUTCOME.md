# Turso 0.7.1 Lab Outcome

## 2026-07-23 — Stable Driver Pass

`tursogo v0.7.1` passed the complete isolated Go-driver suite.

The failed-UPSERT regression produced the expected unique-constraint error.
The original `alice` and `bob` rows were unchanged, forced reads through both
unique indexes returned the correct rows, and `PRAGMA integrity_check` returned
`ok`.

The large literal `IN (...)` regression produced this plan:

```text
SEARCH records USING INDEX records_lookup_key (lookup_key=?)
```

It did not scan the table or covering index.

The 0.7.0 boundaries did not move: experimental plain `WITHOUT ROWID` works but
MVCC writes do not; MVCC and multiprocess WAL remain incompatible; reserved
`BEGIN CONCURRENT` works; Go-driver/MVCC native FTS remains unavailable; the
same-process MVCC write storm passes; sequential child writers pass; overlapping
plain writers need experimental multiprocess WAL.

The optional CLI probes were not run because no matching binary path was
supplied.
