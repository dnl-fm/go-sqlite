# Turso 0.7.0-pre.1 Lab Outcomes

Newest entries go on top. The lab is for engine behavior that is interesting
but not yet a `go-sqlite` API promise.

## 2026-05-20 - Initial 0.7.0-pre.1 Pass

### What We Tested

Turso 0.7.0-pre.1 release notes only ship install text. The tag diff is mostly
MVCC, schema, generated-column, and `printf()` correctness work. The existing
0.6.0 lab questions are still the right probes: `WITHOUT ROWID` and
multi-process access should not be smuggled into `pkg/database` without fresh
evidence.

So this pass added executable probes for both.

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

The multi-process probe starts child test processes. Each child opens the same
database file with the Turso driver and writes its own rows. Sequential child
processes work without extra flags.

| processes | writes per process | expected rows |
|---:|---:|---:|
| 4 | 25 | 100 |

That passed locally on Turso `v0.7.0-pre.1` when children run one after another.
Overlapping child writers are different. Turso documents
`experimental=multiprocess_wal` for inspecting or querying an open `.db` from
another process, but this Go-driver lab still sees WAL locking:

| scenario | result |
|---|---|
| 4 overlapping child writers, no experimental flag | at least one child gets `File is locked by another process` |
| 4 overlapping child writers, `experimental=multiprocess_wal` | still gets `File is locked by another process` on `db.sqlite-wal` |

That does not disprove the core feature. It says this exact `tursogo v0.7.0-pre.1`
path is not enough evidence for `go-sqlite` to promise multiprocess WAL yet.

The matching `tursodb 0.7.0-pre.1` release binary tells the other half of the story:

| setup | result |
|---|---|
| Go app opens `db?experimental=multiprocess_wal` and keeps it open | holder stays live and polls the table |
| `tursodb --experimental-multiprocess-wal db "select count(*) from writes;"` | sees the Go app row |
| `tursodb --experimental-multiprocess-wal db "insert into writes(source) values('tursodb');"` | succeeds |
| Go app keeps polling the table | observes the row count rise to `2` |

So the release-note scenario is real for Go-app-plus-CLI inspection. It is not
the same as many Go processes writing concurrently through `tursogo`.

### What This Means

The dependency bump is safe, but the docs need nuance.

Plain Turso can test and maybe expose experimental `WITHOUT ROWID`. MVCC Turso
still should not promise it. Same-process pooled MVCC writes are fine.
Multi-process WAL is proven for live CLI inspection, but not for arbitrary
concurrent Go writer fleets.
