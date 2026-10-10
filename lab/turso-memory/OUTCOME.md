# Outcome: not a leak — fixed in go-sqlite by tuning GC; VACUUM INTO regressed in 0.8.x

Runs 2026-09-30 to 2026-10-07 on tursogo v0.7.2, v0.8.1 and v0.8.2. Raw numbers:
[results/comparison.md](results/comparison.md) (latest full run, all three
versions) and [results/gosqlite-sweep-comparison.md](results/gosqlite-sweep-comparison.md)
(threshold sweep).

## The short version

Turso MVCC does not leak row versions when it is allowed to collect them. With
one connection, no open reader, and eager checkpoint/GC
(`mvcc_checkpoint_threshold = 0`, `mvcc_gc_threshold = 1`), a 600k-insert
workload with a bounded live set stays flat and trims down to 26–38 MiB on all
three versions. Growth comes from three things:

1. **Turso's default thresholds let versions pile up.** Checkpoint at ~4.12 MB
   of logical log and GC every 16,384 versions leave the same workload at
   313–394 MiB.
2. **glibc keeps what Turso frees.** The shipped `libturso_sync_sdk_kit.so`
   uses system `malloc` (see README). `malloc_trim(0)` and `MALLOC_ARENA_MAX=2`
   both recover a large share of what looked like "retained forever".
3. **An open cursor pins everything.** One `Rows` left open after a single
   `Next()` grows ~1,320–1,355 B/row on every version. By design: the reader's
   snapshot blocks GC.

0.8.1 and 0.8.2 changed none of this.

## The fix in go-sqlite

- **Every pooled connection is configured.** `Open` used to run its PRAGMAs
  once through `*sql.DB`, so only one of the four pooled connections got them;
  the others ran without `foreign_keys = ON`, with Turso's `busy_timeout`,
  `synchronous` and cache. A connector now configures each native connection as
  it opens. `TestOpenConfiguresEveryPoolConnection` fails on the old code
  (`connection 1 foreign_keys = 0, want 1`, same for 2 and 3) and passes now.
- **MVCC thresholds are part of the defaults:** `mvcc_gc_threshold = 1024`,
  `mvcc_checkpoint_threshold = 262144`. Chosen from the sweep below: the most
  memory saved without paying throughput.

Sweep through go-sqlite, pool of 4, 600k inserts, bounded live set:

| Setting | v0.7.2 end / trim MiB | v0.8.1 end / trim MiB | Inserts/s |
|---|---:|---:|---:|
| Turso defaults (16384 / 4120000) | 313 / 94 | 372 / 100 | ~25,600 |
| mild (4096 / 1048576) | 291 / 145 | 341 / 136 | ~26,800 |
| **medium (1024 / 262144) — new default** | **162 / 41** | **195 / 80** | **~26,900** |
| eager (1 / 0) | 92 / 23 | 164 / 40 | ~1,450 |
| Turso defaults + `MALLOC_ARENA_MAX=2` | 181 / 77 | 209 / 103 | ~25,100 |
| medium + `MALLOC_ARENA_MAX=2` | 66 / 43 | 81 / 52 | ~25,300 |

Eager is ruled out: 17–20× slower. The pre-registered rule ("within 1.5× of
eager after trim, ≥ 70% throughput") rejected every pair because eager after
trim is very low; medium was chosen by judgement on the table instead.

Proof run with the new defaults (`gosqlite-steady`, default config, all four
connections read back `foreign_keys=1`, `mvcc_gc_threshold=1024`):

| | v0.7.2 | v0.8.1 | v0.8.2 |
|---|---:|---:|---:|
| End RSS MiB | 171 | 192 | 181 |
| After trim MiB | 48 | 69 | 65 |
| Verdict | FLAT | FLAT | FLAT |

## Still open

- **`VACUUM INTO` regressed in 0.8.x.** On v0.7.2, trim returns RSS to ~26 MiB
  after five passes. On 0.8.x, 401–541 MiB of a 158 MiB database survives trim,
  in all three 0.8.x runs (two on v0.8.1, one on v0.8.2), with no open reader
  or transaction. Worth an upstream issue. We no longer use `VACUUM INTO` for
  snapshots (checkpoint-and-copy replaced it), so the urgency is low.
- **`cache_size` was never the problem.** Each connection's cache stays within
  its limit on clean sequential reads. The 2026-08-06 maild OOM fits MVCC
  retention under constant writes better.

## Limits

One full run per setting and version; small differences between versions are
noise until repeated. `RssAnon` is not a native live-allocation counter. The
workload is a single writer with a bounded live set; long readers, multiple
writers, and other schemas are not covered.
