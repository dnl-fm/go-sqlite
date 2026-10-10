# Turso 0.7.0 Lab Outcomes

Newest entries go on top. The lab is for engine behavior that is interesting
but not yet a `go-sqlite` API promise.

## 2026-07-15 - Final 0.7.0 Pass

### What We Tested

The old prerelease lab questions are still the right probes: `WITHOUT ROWID`,
native FTS, same-process MVCC writes, multi-process Go writers, and matching CLI
behavior should not be smuggled into `pkg/database` without fresh evidence.

### What Happened

`WITHOUT ROWID` has three different answers:

| mode | result |
|---|---|
| plain Turso without flag | rejected with an experimental-feature error |
| plain Turso with `experimental=without_rowid` | create, insert, and select pass |
| MVCC Turso with `experimental=without_rowid` | table creation passes, write fails |

The MVCC failure is the important bit. The old repo wording was too broad, but
the old `WithTursoMVCC()` warning is still directionally correct.

Same-process MVCC autocommit writes are fine under a real pool:

| process shape | connections | writers | writes | result |
|---|---:|---:|---:|---|
| one Go process, Turso MVCC | 32 | 32 goroutines | 1,600 inserts | pass |

That means consumer code should not treat `database/sql` pooling as suspicious
by itself. If the app is one process with one `*sql.DB` using Turso MVCC,
concurrent autocommit writes are supported by the lab evidence.

The final driver also completes a `BEGIN CONCURRENT` transaction on one
reserved connection and reads back the committed row. That keeps the package's
multi-statement MVCC path intact.

The multi-process probe starts child test processes. Each child opens the same
database file with the Turso driver and writes its own rows. Sequential child
processes work without extra flags.

| processes | writes per process | expected rows |
|---:|---:|---:|
| 4 | 25 | 100 |

That passed locally on Turso `v0.7.0` when children run one after another.
Overlapping child writers are different. Turso documents
`experimental=multiprocess_wal` for opening the same `.db` from another process.
Final `v0.7.0` changes the answer for plain-engine child writers:

| scenario | result |
|---|---|
| 4 overlapping child writers, no experimental flag | at least one child gets `File is locked by another process` |
| 4 overlapping child writers, `experimental=multiprocess_wal` | pass; 100 expected rows present |
| `experimental=multiprocess_wal` + `pragma journal_mode='mvcc'` | rejected |

That is the line for this repo. Plain multiprocess WAL is real engine behavior;
it is not a `go-sqlite` MVCC mode.

The matching `tursodb 0.7.0` release binary tells the other half of the
story. The optional probes ran against a matching x86_64 Linux release binary.

| setup | result |
|---|---|
| Go-driver probes | pass |
| matching `tursodb 0.7.0` native FTS plain CLI probe | pass |
| matching `tursodb 0.7.0` plain multiprocess-WAL CLI probe | pass |

The CLI evidence stays in the plain-engine bucket. It does not revive the old
claim that `go-sqlite` MVCC should be opened for raw live-file inspection.

### What This Means

The final 0.7 bump looks safe under the Go-driver probes, but the docs need
nuance.

Plain Turso can test and maybe expose experimental `WITHOUT ROWID`. MVCC Turso
still should not promise it. Same-process pooled MVCC writes are fine. Plain
multi-process WAL is proven for overlapping child writers, but MVCC rejects it,
so `go-sqlite` keeps the owning-process contract.
