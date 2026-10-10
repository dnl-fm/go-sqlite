# Several writers, one SQLite file

A service that was happy with SQLite grows a second thing that writes: a
background job, a webhook handler, another process. A week later the log says
`database is locked (5) (SQLITE_BUSY)` and somebody proposes Postgres.

SQLite in WAL mode lets exactly one connection write at a time, while any
number read. That rule never bends, so the only design decision is *where the
other writers wait*. They can wait in Go, where waiting is cheap, cancellable
and yours to order. Or they can wait inside SQLite's busy handler, which is a
sleep-and-retry loop with no line and a five second fuse.

**Wait in Go.** Open the file twice: one `*sql.DB` with exactly one connection
does all the writing, a second, read-only pool does all the reading. That is
`open.go`. Everything else in this lab is the evidence, on
`modernc.org/sqlite` v1.60.1.

## What we support

The lab pushes these patterns far past anything we run. What we *promise* a
project is much smaller, on purpose:

| | Supported | Measured ceiling on the devbox (tiny writes) |
|---|---|---|
| Writers | one process owns the file; all writes go through it | — |
| Sustained writes | **up to 1,000/s per database** | C ~12,000/s, D ~77,000/s (`NORMAL`); D ~9,000/s (`FULL`) |
| Bursts | **up to ~5,000/s for a few seconds** | D drained 200% bursts in 4–6.5 s |
| Write wait | **at most ~1 s, then `context.DeadlineExceeded`** | 50% load: p99 26–37 ms, max ≤ 95 ms |
| Overload | **rejected fast; the caller says "retry later"** (HTTP 503 + `Retry-After`, client backoff with jitter) | rejections in ~100–115 ms |

A rejection is the honest answer. Making a caller wait 5 s for a write that
may still time out is not a feature.

Outside that envelope a project runs this lab's load scenario on its own
workload before relying on SQLite: sustained writes above 1,000/s, write
transactions bigger than a few small rows, several writing processes, long
read transactions, or durability promises beyond SQLite's documented ones.
If the answer is "tens of thousands of writes per second", "several machines
must write", or "must survive losing the box", pick a different database
(Postgres).

## Which pattern

| Your situation | Use | What it buys | What it costs |
|---|---|---|---|
| Several goroutines write, one process. **The default.** | **C split**: `Open` + `Store.Write` | No lock errors, no lost updates, WAL stays at 4 MB while reads are short, readers never wait | Writers get the connection in random order |
| Sustained write traffic, or writes must not wait long under load | **D queue**: C + `Queue` | FIFO: no caller waits through many commits. Passed the whole load contract at 50% (see Under load) | 1 ms batching wait per write |
| Many tiny writes and the commit must survive power loss (`synchronous=FULL`), or writes must be served in arrival order | **D queue**: C + `Queue` | One fsync per batch instead of per write: 12x the throughput under FULL. Strict FIFO | 1 ms added to every write; a pure tax under `synchronous=NORMAL` |
| Writers live in separate processes that cannot be merged | **E multi-process**: `Open` in every process | Correct for short transactions | Lock waits are polled and unfair, the WAL never resets under load, and a long transaction in one process is `SQLITE_BUSY` in another |
| One shared pool for everything | **Don't** (A, B below) | | |

E is a last resort, not a peer of C. If you can route the writes through one
owning process, do that and use C.

## Under load

**Plan a single writer at ≤50% of measured capacity for tight tails.
Use D for high throughput, especially with FULL durability.
The remaining red cells stay red.**

`TestScenarioLoad` schedules requests independently and includes generator
lag in caller latency. The workload is 90% 512-byte inserts and 10%
read-modify-write across 64 hot rows. Eight point readers pause 1ms between
requests. The disk probe and all databases use ext4/NVMe `.labdata/`.

At scenario start, 200 separate 4KiB writes to a fresh file each precede an
`fdatasync`. The probe measures sync p50/p99/max. Two full runs on
2026-10-10 measured these values:

| Run | Sync p50 ms | Sync p99 ms | Sync max ms | Write p99 bound ms |
|---|---:|---:|---:|---:|
| 1 | 5.94 | 11.98 | 27.26 | 35.95 |
| 2 | 6.10 | 17.93 | 23.77 | 53.80 |

The 60s sustained cells run at 50% or 80% of calibrated capacity.
At 50%, C-NORMAL, D-NORMAL and D-FULL require write p99 ≤3 × sync p99,
write max ≤250ms, and reader p99 ≤10ms. Both rates require zero rejections,
exact accepted writes, zero unexpected or reader errors, and commit gap ≤250ms.
At 80%, latency is recorded. C-FULL sustained tails and deadlines remain
recorded-only. Its integrity and commit progress remain asserted.
All six 50% latency cells passed across the two runs.

Both full burst cells run for 13s on 50% background. The spike starts at 1s
and offers 200% total capacity for 2s. Then 10s remain at 50%.
For calibrated capacity C, the spike adds `(2 − 0.5) × C × 2s = 3C`
requests above background. With `0.5C` spare capacity, the drain budget is
about 6s. Actual backlog depends on service capacity during the spike.

The `burst` contract requires zero rejections, exact writes, write max ≤5s,
and commit gap ≤250ms. Both of its request contexts allow 5s.
Recovery requires two complete post-spike windows with write p99 back at
the sustained bound. Drain includes the windows closing and their last
request completion. Recovery must occur within the cell and ≤8s after spike end.

The identical `burst-short-deadline` uses 1s deadlines. Every rejection must
satisfy `errors.Is(err, context.DeadlineExceeded)` and return within 1050ms.
Accepted writes must land exactly, without unexpected errors or commit stalls.

The 20s overload runs at 150%. It requires accepted p99 ≤250ms, typed
rejection within 150ms of scheduled arrival, no commit stall, and a
heap/goroutine plateau. It uses a 100ms child admission deadline inside the
caller's 1s deadline.

Each full calibration used 64 closed-loop submitters for 10s. Capacity counts
acknowledged requests, not batch transactions. Paired values mean run 1 / run 2.

| Pattern | Capacity requests/s | p99 at 50% ms | p99 at 80% ms | Rejections at 80% | Result |
|---|---:|---:|---:|---:|---|
| C-NORMAL | 12544.70 / 11298.60 | 25.30 / 35.90 | 671.70 / 244.30 | 6254 / 57 | 50%: PASS / PASS; 80%: RED / RED |
| D-NORMAL | 76434.70 / 76940.70 | 26.60 / 26.90 | 28.60 / 988.60 | 0 / 13835 | 50%: PASS / PASS; 80%: PASS / RED |
| C-FULL | 157.60 / 159.30 | 12.00 / 12.80 | 19.30 / 76.50 | 0 / 0 | Integrity/progress pass; tails/deadlines recorded |
| D-FULL | 9072.10 / 7897.30 | 27.40 / 36.90 | 98.00 / 28.00 | 0 / 0 | 50%: PASS / PASS; 80%: PASS / PASS |

| Pattern | 5s burst rejected | Write max ms | Observed drain | 5s result | 1s burst rejected | Rejection max ms |
|---|---:|---:|---|---|---:|---:|
| C-NORMAL | 4188 / 1668 | 5015.46 / 4999.67 | 7.50s / 6.00s | RED / RED | 24295 / 17910 | 1005.85 / 1003.75 |
| D-NORMAL | 0 / 0 | 2897.63 / 2884.84 | 6.50s / 6.51s | PASS / PASS | 111845 / 120906 | 1002.09 / 1011.83 |
| C-FULL | 0 / 4 | 4910.28 / 4986.94 | 4.25s / 5.26s | PASS / RED | 209 / 253 | 1001.08 / 1001.08 |
| D-FULL | 0 / 0 | 2042.12 / 1607.94 | 4.76s / 4.03s | PASS / PASS | 13307 / 6735 | 1001.26 / 1001.20 |

All eight 5s bursts met the recovery bound. D-NORMAL and D-FULL also met
the zero-rejection and write-max requirements in both runs.
Every short-deadline burst and overload cell passed its contract.
Both runs had exact accepted inserts and per-row counters, zero unexpected
write errors, and zero reader errors. The largest commit gap was 97.19ms,
and reader p99 stayed ≤0.30ms.

**Remaining red cells:** C-NORMAL at 80% and its 5s burst in both runs.
D-NORMAL at 80% and C-FULL's 5s burst also failed in run 2.
These cells rejected requests. C-NORMAL's first burst also exceeded the 5s
write maximum by 15.46ms. Its recovery passed in 7.50s / 6.00s.
C-FULL's second burst recovered in 5.26s but rejected four requests.

C's random pool order permits individual callers to wait across many commits.
Short commit gaps therefore do not guarantee a caller deadline.
D-NORMAL's second 80% cell offered 61,552.56 requests/s and reached 61,655
pending requests. It rejected 13,835 requests despite continued commits.
Calibration does not guarantee later capacity or enough spare capacity for
short queue waits.

The logs do not isolate disk, generator, or host scheduling
as the cause of each delay. All bursts observed recovery within the existing bound.

The default short suite and `-race` assert correctness, exact counts, commit
progress, expected deadline behavior, and fast typed rejections.
They record latency, drain and memory trends. Full mode additionally asserts
50% sustained latency, burst latency/recovery, and overload latency/plateau.
Ten final short suites passed in 44.8–45.2s, and one race suite passed in 58.9s.

Ten baseline short runs also passed. The earlier uncaptured flake did not
recur, so its cause remains unknown. No flake fix is claimed.

Choose a caller deadline longer than the expected backlog plus commit time.
For example, a 1s deadline cannot absorb 2s of backlog.
Treat `DeadlineExceeded` as "retry later" and use backoff with jitter.
Apply the deadline to `Store.Write` or `Queue.Submit`.
Once a queued operation starts, `Queue.Submit` waits for its actual commit outcome.
Slow callbacks therefore need a separate budget.

These runs do not prove crash durability, slow callbacks, scan readers,
Litestream interaction, or other hardware. Commit times use the first
acknowledgement of each transaction as an approximation. The finite memory
check cannot rule out small leaks. Histograms round p99 up to 100us and use
the maximum conservatively when p99 reaches the 1s overflow bin.
Full rows and samples are in [the load record](results/latest.md#load).
Short and race runs never replace it.

## What the lab measured

16 writer goroutines and 8 readers against one file, on the devbox's ext4
RAID1 (not tmpfs: fsync has to cost something). Full run of 2026-10-07; three
other labs were benchmarking on the same machine, so read ratios, not
absolutes. Latencies in milliseconds. `lost` is acknowledged writes missing
from the database. `wal MB` is the `-wal` file when the writers finished.

| workload | strategy | ops | busy | other | lost | ops/s | write p50 | write p99 | write max | read p50 | read p99 | wal MB |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| inserts | A naive | 160000 | 0 | 0 | 0 | 19667 | 0.29 | 6.07 | 629.12 | 0.99 | 8.31 | 1387.8 |
| inserts | B immediate | 160000 | 0 | 0 | 0 | 19552 | 0.30 | 5.99 | 631.77 | 1.03 | 8.31 | 1386.1 |
| inserts | C split | 160000 | 0 | 0 | 0 | 8350 | 0.48 | 29.76 | 150.75 | 0.28 | 6.66 | 4.5 |
| inserts | D queue | 160000 | 0 | 0 | 0 | 6809 | 1.68 | 14.75 | 35.61 | 0.29 | 5.95 | 6.2 |
| inserts | E multi-process | 160000 | 0 | 0 | 0 | 27577 | 0.08 | 7.38 | 1737.09 | 0.24 | 1.70 | 776.4 |
| inserts | C split (FULL) | 8000 | 0 | 0 | 0 | 162 | 67.46 | 425.54 | 790.86 | 0.02 | 0.10 | 3.9 |
| inserts | D queue (FULL) | 8000 | 0 | 0 | 0 | 2020 | 7.64 | 14.46 | 20.56 | 0.02 | 0.11 | 4.0 |
| counter | A naive | 34303 | 45697 | 0 | 0 | 30577 | 0.16 | 0.92 | 20.04 | 0.32 | 1.24 | 134.1 |
| counter | B immediate | 80000 | 0 | 0 | 0 | 38678 | 0.19 | 1.92 | 529.23 | 0.46 | 2.44 | 313.6 |
| counter | C split | 80000 | 0 | 0 | 0 | 25476 | 0.23 | 19.71 | 27.36 | 0.02 | 0.11 | 3.9 |
| counter | D queue | 80000 | 0 | 0 | 0 | 9172 | 1.46 | 2.29 | 37.47 | 0.02 | 0.07 | 3.9 |
| counter | E multi-process | 80000 | 0 | 0 | 0 | 36213 | 0.07 | 6.67 | 1229.57 | 0.03 | 0.08 | 161.8 |
| long-tx | A naive | 1281 | 2 | 0 | 0 | 127 | 0.17 | 504.65 | 4336.73 | 99.87 | 619.40 | 9.3 |
| long-tx | B immediate | 590 | 5 | 0 | 0 | 59 | 100.43 | 703.57 | 4436.13 | 100.86 | 803.29 | 3.9 |
| long-tx | C split | 956 | 0 | 0 | 0 | 95 | 100.34 | 604.05 | 805.87 | 0.01 | 0.04 | 3.9 |
| long-tx | D queue | 891 | 0 | 0 | 0 | 88 | 101.70 | 114.90 | 114.95 | 0.01 | 0.04 | 1.3 |
| long-tx | E multi-process | 659 | 4 | 0 | 0 | 65 | 0.05 | 4231.20 | 10007.54 | 0.01 | 0.04 | 3.9 |

The three workloads: `inserts` is 10,000 single-row inserts per writer.
`counter` is 5,000 read-modify-write increments of one row per writer (`SELECT
v`, then `UPDATE … SET v = ?`, same transaction). `long-tx` is one writer
holding a transaction for 100 ms, over and over, while 8 others insert for
10 s. The `(FULL)` rows repeat `inserts` with `synchronous=FULL` and 500
inserts per writer.

Read the last column before the `ops/s` column. A, B and E post the best
insert throughput and end with a WAL between 0.8 and 1.4 GB: it never starts
over. They do checkpoint, on whichever connection just committed, while the
other connections keep writing. C checkpoints on its only writer connection,
so every writer waits for it, and its WAL is 4 MB.

## Copy it

```bash
cp lab/sqlite-writers/open.go  yourapp/internal/store/
cp lab/sqlite-writers/queue.go yourapp/internal/store/   # pattern D only
go get modernc.org/sqlite@v1.60.1
```

Change the `package` line. `open.go` stands alone; `queue.go` needs `open.go`.
`open_test.go` and `queue_test.go` travel with them (the queue tests use the
helpers in `open_test.go`). The scenario tests stay here.

```go
store, err := writers.Open(ctx, "/srv/prod/app/data/app.db", writers.Options{})
defer store.Close()

// Write: one transaction on the one writer connection. It is BEGIN IMMEDIATE,
// so read-then-write inside it is safe.
err = store.Write(ctx, func(tx *sql.Tx) error {
	var v int64
	if err := tx.QueryRow("SELECT v FROM counter WHERE id = 1").Scan(&v); err != nil {
		return err
	}
	_, err := tx.Exec("UPDATE counter SET v = ? WHERE id = 1", v+1)
	return err
})

// Read: plain queries on store.Reader, or store.Read for several statements
// that must see one snapshot.
err = store.Reader.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&n)

// Pattern D. Close the queue before the store.
q := writers.NewQueue(store, writers.QueueOptions{}) // MaxBatch 64, MaxWait 1ms
defer q.Close()
err = q.Submit(ctx, func(tx *sql.Tx) error { /* same body as Write */ })
```

Three rules come with the code. Never call `Write` or `Submit` from inside a
`Write` or `Submit` callback: there is one writer connection and you are
holding it, so the inner call waits forever. Never do slow work (HTTP, sleep,
a big read) inside a write callback: every other writer is behind you. And
set the pragmas in the DSN, as `open.go` does, never with `db.Exec("PRAGMA
…")`: that configures whichever pooled connection happened to run it.

## What each pattern guarantees

**C split.** Each line is an assertion in the tests.

- Writers in one process cannot get `SQLITE_BUSY` from each other. 160,000
  inserts and 80,000 read-modify-write increments from 16 goroutines: zero
  errors, zero lost updates.
- Every pooled connection carries the pragmas. The test reserves all reader
  connections at once and reads `foreign_keys`, `busy_timeout`,
  `journal_mode` and `temp_store` back from each.
- The reader pool cannot write: `attempt to write a readonly database (8)`.
  It is opened with `mode=ro`, not `PRAGMA query_only`, because a statement
  can switch `query_only` off again.
- Readers do not wait for the writer. With a writer holding the lock for
  100 ms at a time, reader p99 was 0.04 ms. The test fails at 50 ms.
- `Write` rolls back on error and on panic, and the panic keeps propagating.
- `Checkpoint` empties the WAL. Behind a read transaction that never ends it
  reports busy after its window instead of holding the write lock for
  `busy_timeout`. The test fails at one second.

What C does not give you is order. `database/sql` hands a free connection to
a *random* waiter, not the oldest. Behind the 100 ms transaction the median
writer waited one turn, p99 waited six (604 ms) and the unluckiest waited
806 ms.

**D queue.** Everything in C, plus:

- `Submit` returning nil means the batch holding your op has committed.
- Ops run in arrival order. Same long-transaction workload: p99 115 ms,
  max 115 ms. Nobody waits more than one turn.
- Each op runs in its own `SAVEPOINT`. One failing op among 64 in a batch:
  exactly one error, 63 rows. A panicking op becomes that op's error and the
  queue goroutine survives.
- If the batch itself dies, every op in it gets the error and none is
  applied.
- Context cancelled while the op is still queued: it never runs, `Submit`
  returns `ctx.Err()` at once. Cancelled after it started: `Submit` waits and
  returns the real outcome. You always know whether the write happened.
- `Close` runs everything already queued; `Submit` afterwards returns
  `ErrQueueClosed`.

The costs: every write waits up to `MaxWait` for company (write p50 1.68 ms
against C's 0.48 ms), and one slow op stalls its whole batch. Inside an op,
use the `tx` without a cancellable context. SQLite rolls back the *entire*
transaction when a write statement is interrupted, so one cancelled statement
fails the batch for everyone in it.

**E multi-process.** Zero errors and zero lost updates on `inserts` and
`counter`, four processes writing at once. That is all it guarantees. On
`long-tx` writers in other processes hit `SQLITE_BUSY` once `busy_timeout`
runs out (4 of 659 writes, 6 of 9 runs). The scenario records that as the
finding instead of failing; lost writes and non-busy errors still fail it.
See the next section.

## How the others fail

**A naive** (one pool of 4, default `BEGIN`). The counter workload lost
45,697 of 80,000 increments to:

```
database is locked (5) (SQLITE_BUSY)
```

Two transactions read the counter from the same snapshot. One gets the write
lock and commits. The other now wants to write on top of a snapshot that is
stale, and no amount of waiting fixes that, so SQLite fails it immediately;
`busy_timeout` never comes into it. Note what did *not* happen: no update was
lost. SQLite refuses rather than corrupts. You just get an error rate of
57% on a routine operation. A was clean on `inserts`, and that is
real, not luck: a transaction whose first statement is the write has no
snapshot to go stale.

**B immediate** (same pool, `_txlock=immediate`). This fixes the counter and
is where most codebases stop. Three things are still wrong, because four
connections still fight over one lock inside SQLite:

- A long transaction starves the others. `long-tx` produced 5
  `database is locked (5) (SQLITE_BUSY)` errors under B and 2 under A, each
  after waiting out the five second `busy_timeout`. SQLite's busy handler sleeps and retries
  with no queue, so whoever asks at the right microsecond wins.
- Readers share the pool with writers that are stuck in that handler. Reader
  p99 during `long-tx`: 803 ms, against 0.04 ms for C.
- The WAL never resets (next section): 1.4 GB after 160,000 inserts.

**E multi-process with a long transaction.** Each process is a well-behaved
C, and across processes they are B: several writer connections polling for
one lock. In the full run `long-tx` under E returned 4 `SQLITE_BUSY` errors
and one write waited 10 s. Across nine full-mode runs of that cell, six had
lock errors. The test asserts zero, so **the full-mode suite is
red on `long-tx/E multi-process` most of the time, and that failure is the
finding**: with several processes, one process's 100 ms transactions are
another process's five second timeout. The short mode passes that cell only
because its 1.5 s run is shorter than `busy_timeout`.

## The WAL rules

Watch one number in production: the size of `app.db-wal`. In a healthy C
store it sits at 4 MB. Alert at 100 MB.

**The rule: the WAL starts over only at an instant when a checkpoint has
copied all of it into the database and no reader is still on an older
snapshot. Whatever keeps that instant from coming makes the file grow by
everything you write.** Three things do. One that looks like it should does
not.

The matrix: 4 writer goroutines are offered 800 commits per second of 2 KB
rows for 30 s, through one writer connection (C, `Open`) or a pool of four
(B). The 8 readers always have their own read-only pool, so the writer
connections are the only difference inside a pair of rows. `restarts` is how
often the WAL started over. `not checkpointed` is how much of the WAL no
checkpoint had copied yet, the median of one sample per second. Sizes in MB,
latencies in ms. Run of 2026-10-08.

| readers | writer conns | commits | write p99 | reads | read p99 | reads in flight | wal @10s | wal @30s | restarts | not checkpointed |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| none | 1 (C) | 22,350 | 23.86 | 0 | 0.00 | 0.00 | 3.9 | 4.0 | 93 | 1.9 |
| none | 4 (B) | 23,997 | 5.26 | 0 | 0.00 | 0.00 | 131.1 | 395.1 | 0 | 0.1 |
| point | 1 (C) | 22,316 | 23.77 | 207,143 | 0.10 | 0.23 | 4.0 | 4.0 | 93 | 2.5 |
| point | 4 (B) | 23,996 | 5.25 | 206,714 | 0.12 | 0.26 | 130.9 | 394.6 | 0 | 0.1 |
| scan | 1 (C) | 22,149 | 24.20 | 162,763 | 0.55 | 0.97 | 4.0 | 4.0 | 92 | 1.8 |
| scan | 4 (B) | 23,997 | 5.46 | 160,850 | 0.63 | 1.16 | 130.9 | 394.6 | 0 | 0.1 |
| longscan | 1 (C) | 21,652 | 12.53 | 11,565 | 60.95 | 7.54 | 100.5 | 348.7 | 2 | 0.4 |
| longscan | 4 (B) | 23,927 | 9.81 | 9,054 | 81.07 | 7.64 | 130.3 | 393.7 | 0 | 0.4 |
| overlap | 1 (C) | 21,082 | 25.67 | 14,510,914 | 0.08 | 7.97 | 4.2 | 4.7 | 86 | 1.8 |
| overlap | 4 (B) | 23,987 | 5.29 | 14,447,981 | 0.08 | 7.97 | 130.8 | 394.2 | 0 | 0.1 |
| pinned | 1 (C) | 23,935 | 0.10 | 0 | 0.00 | 0.00 | 130.8 | 394.5 | 0 | 209.8 |
| pinned | 4 (B) | 23,967 | 0.10 | 0 | 0.00 | 0.00 | 131.1 | 395.0 | 0 | 210.2 |

The reader patterns: `point` is a point lookup, then a 1 ms pause. `scan`
adds a `count(*)`, which SQLite answers from the small UNIQUE index. `longscan`
forces that count over the table itself, 2 KB a row, so the read gets longer
as the table grows. `overlap` is point lookups with no pause. `pinned` is one
read transaction held open and no other reader.

**Several writer connections: zero restarts, with or without readers.** Every
B row ends near 394 MB, 13 MB a second, including the row with no reader at
all. With short reads or none, `not checkpointed` is 0.1 MB: the checkpoints
run and they keep up. What never happens is the WAL being *completely* copied at the moment the next
writer begins, because while one connection checkpoints, at two fsyncs a
time, the other three commit. One writer connection cannot have that
problem. It checkpoints, then it writes: 93 restarts, 4.0 MB.

**Reads long enough to span commits: two restarts, then none.** Same single
writer connection, readers scanning the table: 349 MB. Both restarts came in
the first second, while the table was small and the scan short. After that
some reader was always on a snapshot older than the latest commit, so every
checkpoint stopped just short of the end (0.4 MB), and the WAL never started
over.

**Short reads are harmless, however many.** `overlap` kept 7.97 of its 8
readers in flight for the whole cell, 14.5 million reads: 86 restarts,
4.7 MB. A read that is always open is not the problem. A read that is still
open when the next commit lands is: these last 0.08 ms and commits are
1.25 ms apart. The index `count(*)` at 0.55 ms is as harmless. Where the line
runs between 0.6 ms and 60 ms the lab did not map.

**A read transaction left open: nothing is checkpointed at all.** `pinned`
ends at 394 MB with the whole file waiting (the median sample is half the
final size). One idle `BEGIN; SELECT` did that, and a `Rows` nobody closed
does the same. `Store.Read` exists so the transaction ends when the function
returns.

`restarts` is not a guess. SQLite adds one to a counter in the wal-index
header (salt-1) every time the WAL starts over; the lab reads that header
from the `-shm` file once a second, so counting costs the workload nothing.

### What to do about it

Each fix was tried on a cell that grew for a different reason. Timed
checkpoints run every 5 s; `Store.Checkpoint` gets a 50 ms window unless the
row says otherwise.

| cell | mitigation | commits | write p99 | write max | read max | wal max | restarts | timed checkpoints | busy | slowest |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| B, readers point | none | 23,996 | 5.25 | 16.75 | 5.39 | 394.6 | 0 |  |  |  |
| B, readers point | journal_size_limit=16MiB | 23,997 | 5.26 | 32.71 | 5.36 | 395.4 | 0 |  |  |  |
| B, readers point | no auto-checkpoint, RESTART every 5s | 23,713 | 0.14 | 128.65 | 0.53 | 65.2 | 5 | 6 | 0 | 79.43 |
| B, readers point | no auto-checkpoint, TRUNCATE every 5s | 23,654 | 0.15 | 129.65 | 1.86 | 65.0 | 6 | 6 | 0 | 100.42 |
| B, readers point | Store.Checkpoint every 5s | 23,916 | 5.28 | 103.59 | 65.71 | 196.9 | 3 | 6 | 3 | 65.62 |
| B, readers point | no auto-checkpoint, Store.Checkpoint every 5s | 23,639 | 0.15 | 128.64 | 99.63 | 65.0 | 6 | 6 | 0 | 99.60 |
| C, readers longscan | none | 21,652 | 12.53 | 61.34 | 66.67 | 348.7 | 2 |  |  |  |
| C, readers longscan | journal_size_limit=16MiB | 21,920 | 11.87 | 46.66 | 68.68 | 353.0 | 2 |  |  |  |
| C, readers longscan | no auto-checkpoint, RESTART every 5s | 19,506 | 3.39 | 5128.81 | 78.73 | 64.9 | 4 | 6 | 1 | 5077.00 |
| C, readers longscan | no auto-checkpoint, TRUNCATE every 5s | 23,238 | 3.35 | 290.27 | 91.09 | 64.9 | 6 | 6 | 0 | 280.96 |
| C, readers longscan | Store.Checkpoint every 5s | 17,901 | 26.87 | 100.58 | 109.99 | 60.2 | 12 | 6 | 0 | 58.16 |
| C, readers longscan | no auto-checkpoint, Store.Checkpoint every 5s | 23,648 | 3.42 | 99.78 | 172.69 | 64.8 | 6 | 6 | 0 | 140.95 |
| C, readers longscan | Store.Checkpoint every 5s, 5ms window | 21,496 | 15.13 | 69.17 | 79.22 | 251.3 | 5 | 6 | 4 | 43.30 |
| C, readers pinned | none | 23,935 | 0.10 | 23.00 | 0.00 | 394.5 | 0 |  |  |  |
| C, readers pinned | journal_size_limit=16MiB | 23,935 | 0.10 | 23.19 | 0.00 | 393.4 | 0 |  |  |  |
| C, readers pinned | no auto-checkpoint, RESTART every 5s | 4,013 | 0.10 | 20026.93 | 0.00 | 65.1 | 0 | 12 | 12 | 5067.07 |
| C, readers pinned | no auto-checkpoint, TRUNCATE every 5s | 4,015 | 0.10 | 15086.59 | 0.00 | 65.1 | 0 | 6 | 6 | 5072.83 |
| C, readers pinned | Store.Checkpoint every 5s | 23,759 | 0.10 | 51.10 | 0.00 | 390.7 | 0 | 6 | 6 | 50.84 |
| C, readers pinned | no auto-checkpoint, Store.Checkpoint every 5s | 23,748 | 0.10 | 96.28 | 0.00 | 390.2 | 0 | 6 | 6 | 98.26 |

**`journal_size_limit` caps nothing.** It trims the file when the WAL starts
over, and the problem is that it does not: 395, 353 and 393 MB with it.

**Several writer connections: use one.** That is `Open`, and there is nothing
else to do. If you cannot (pattern E), turn the automatic checkpoint off on
*every* writer connection (`wal_autocheckpoint=0` in the DSN pragma list) and
let exactly one timer run `PRAGMA wal_checkpoint(TRUNCATE)`. 6 of 6
succeeded, the WAL peaked at 65 MB, which is five seconds of writes, and
write p99 fell from 5.25 to 0.15 ms. The price is one write of about 130 ms
per checkpoint. Leave the automatic checkpoint on and the timer loses to it
(the plain `Store.Checkpoint` row): 3 of 6 busy in this run, 6 of 6 in the
run before.

**Long reads on one writer connection: `Store.Checkpoint` on a timer.**

```go
// One goroutine per store. Stop it before store.Close.
tick := time.NewTicker(5 * time.Second)
defer tick.Stop()
for {
	select {
	case <-ctx.Done():
		return
	case <-tick.C:
	}
	busy, err := store.Checkpoint(ctx, 50*time.Millisecond)
	if err != nil || busy {
		log.Printf("checkpoint: busy=%t err=%v", busy, err)
	}
}
```

It holds new `Store.Read` calls back, gives the running ones the window to
finish, then runs a TRUNCATE checkpoint on the writer connection. 6 of 6
succeeded and the WAL stopped at 60 MB instead of 349 and climbing. The
slowest call took 58 ms, the longest read 110 ms against 67 without. It only
holds back what goes through `Store.Read`, so that is where long reads
belong, not on `store.Reader`. Commits were 17,901 in this run and 21,933 in
the run before, against 21,652 with no fix, so do not read a cost or a gain
into that. Turning the automatic checkpoint off as well
(`store.Writer.Exec("PRAGMA wal_autocheckpoint=0")` right after `Open`) was
better for the writers both times, 23,648 commits and a write p99 of 3.4 ms,
but then this timer is the only thing between you and an unbounded WAL.

**The window has to be about as long as the reads.** Running reads get two
windows to finish: one while writes still flow, a second with the write lock
held. 50 ms was enough for these 60 ms reads. 5 ms was not: 4 of 6 busy,
251 MB. Under the race detector, where the same scan takes one to three
seconds, the 50 ms window was busy 6 times of 6. A busy call is cheap (the
longest write was 69 ms), it just does not help. So set the window to your
longest ordinary read, and know that a new read may wait that long once per
tick. Long reads have that price. The other way out is to stop them
overlapping.

**Do not put a plain `wal_checkpoint(RESTART)` or `(TRUNCATE)` timer on a
store with long reads.** It caps the WAL as well, but a checkpoint that
cannot get past the readers waits out `busy_timeout` *holding the write
lock*. RESTART did that once in this 30 s cell: one write waited 5.1 s.
TRUNCATE got away with it here and did the same in the two runs before.

**A leaked read transaction turns that timer into an outage.** Every
checkpoint waits five seconds with the write lock: 4,013 commits instead of
23,935, and one write waited 20 s. `Store.Checkpoint` gives up after its
window: 23,759 commits, longest write 51 ms. The WAL still grows to 391 MB,
because nothing can checkpoint past an open snapshot. Ending the transaction
is the only fix, and `busy` on every tick is how you find out.

### Why two builds of this lab disagreed

An earlier build of this lab, on tmpfs, measured the opposite: C growing to
615 MB, B flat at 40 MB. Both builds were right about what they ran.

- **B.** Here it grows because a checkpoint costs two fsyncs and another
  connection always commits in the meantime. On tmpfs an fsync is free and
  the checkpoint wins. The other build's B cell, rebuilt on this disk, never
  restarted (209 MB after 60 s); with `synchronous=OFF` it restarted 11 times
  and sat at 100 MB.
- **C.** There it grew because its `count(*)` was a long table scan, eight at
  a time. This lab's `count(*)` is a sub-millisecond index scan. The same
  cell gives 4.1 MB and 199 restarts with the index, 723 MB and 2 restarts
  with the count forced over the table.

| cell | seconds | commits | read p99 | wal @10s | wal @20s | wal at end | restarts |
|---|---:|---:|---:|---:|---:|---:|---:|
| this lab: B immediate, inserts burst | 8 | 160,000 | 8.29 |  |  | 1385.9 | 1 |
| other build: C split, scan readers | 60 | 47,611 | 0.85 | 4.0 | 4.0 | 4.1 | 199 |
| other build: C split, scan readers, count over the table | 60 | 44,433 | 122.97 | 75.3 | 188.3 | 723.1 | 2 |
| other build: B immediate on one shared pool, count over the table | 60 | 12,758 | 73.36 | 66.6 | 105.5 | 209.3 | 0 |
| other build: B immediate on one shared pool, count over the table, synchronous=OFF | 60 | 14,783 | 63.33 | 75.1 | 100.3 | 100.3 | 11 |

The other build is deleted. That its count ran over the table and that tmpfs
is what saved its B are reconstructions that reproduce its numbers, not
facts read from its code.

### The earlier runs

The first version of this section rested on 60 s runs at about 1,500 commits
per second. They agree with the matrix and are kept as a second measurement.

| variant | writes/s | errors | write p99 ms | read p99 ms | wal @10s | @20s | @30s | @40s | @50s | wal max | wal at end | after TRUNCATE | notes |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|
| C split | 1595 | 0 | 11.93 | 2.03 | 3.9 | 4.0 | 4.0 | 4.0 | 4.1 | 4.1 | 4.1 | 0.0 |  |
| C split, readers never pause | 1074 | 0 | 32.37 | 0.58 | 4.0 | 4.4 | 4.8 | 5.6 | 7.1 | 9.5 | 9.5 | 0.0 |  |
| C split, one read tx held open | 1778 | 0 | 0.49 | 5.02 | 152.0 | 306.2 | 461.4 | 613.7 | 765.3 | 914.8 | 914.8 | 0.0 | checkpoint with the read tx still open: busy=true, wal still 914.8 MB |
| B immediate (4 writer conns) | 1207 | 0 | 10.85 | 1.34 | 99.6 | 203.3 | 308.8 | 415.5 | 521.2 | 625.2 | 625.2 | 0.0 |  |
| B immediate + TRUNCATE checkpoint every 5s | 1207 | 0 | 10.87 | 1.28 | 99.5 | 52.6 | 157.6 | 53.2 | 159.4 | 263.4 | 263.4 | 0.0 | 12 timed checkpoints, 10 busy |
| B immediate, wal_autocheckpoint=0 + TRUNCATE checkpoint every 5s | 1443 | 0 | 3.29 | 2.76 | 62.3 | 64.3 | 66.6 | 63.9 | 60.6 | 66.6 | 0.0 | 0.0 | 12 timed checkpoints, 0 busy |

A reader pool running flat out cost the writer a third of its throughput
there (1,074 against 1,595 writes per second). At the matrix's 800 it cost
6% (21,082 against 22,350 commits). The WAL stayed bounded both times.

**Checkpoint through `Writer`.** `Reader` is read-only all the way down, so a
checkpoint through it fails with `disk I/O error (778)`. That is also why
`Store.Close` closes the reader pool first: the writer must be the last
connection on the file to do the final checkpoint.

**The checkpoint is on your write path.** In C the writer pays for it: two
fsyncs per 4 MB of WAL. That is most of the gap between C's 8,350 inserts per
second and B's 19,552, and it is C's write p99 of 30 ms. In the matrix it is
why 22,350 of the 24,000 offered commits got through, at a write p99 of
24 ms. A larger `wal_autocheckpoint` trades WAL size for fewer fsyncs. The
cells with it off and a 5 s timer are the far end of that trade: write p99
0.15 ms, 65 MB of WAL, one write of about 130 ms per checkpoint.

## Durability: NORMAL or FULL

`Open` defaults to `synchronous=NORMAL`. A commit is written to the WAL but
not fsynced; the fsync happens at the next checkpoint. An acknowledged write
survives the application crashing or being killed. On an OS crash or power
loss the most recent commits can be gone. The database is still consistent,
just slightly in the past.

`Options{Synchronous: "FULL"}` fsyncs the WAL on every commit. An
acknowledged write then survives power loss, and you pay for it on every
write: on this disk C managed 162 writes per second with a median wait of
67 ms, 16 writers standing in line for one fsync each.

That is the case the queue is for. Same disk, same durability, D committed
2,020 writes per second at a median of 7.6 ms, because sixteen writers
share one fsync. Under NORMAL there is no fsync to share, and the queue's 1 ms
wait makes it slower than C (9,172 against 25,476 increments per second).

Use FULL when the acknowledgement triggers something you cannot take back
outside the database: money moved, a mail sent, another system told. Use it
with the queue. Everything else runs on NORMAL with plain C.

## Running the lab

```bash
cd lab/sqlite-writers
GOWORK=off go vet ./... && GOWORK=off go test -count=1 ./... && GOWORK=off go test -race -count=1 ./...
LAB_FULL=1 GOWORK=off go test -run Scenario -v -timeout 60m ./...
LAB_FULL=1 GOWORK=off go test -run TestScenarioWALMatrix -v -timeout 60m ./...   # the wal-matrix alone
LAB_FULL=1 GOWORK=off go test -count=1 -run TestScenarioLoad -v -timeout 60m ./... # load alone; contract failures stay red
```

The default run is short mode. Load calibration runs for 500ms, sustained
cells for 1.5s, each burst for 1s with a 200ms spike, and overload for 1.5s.
Ten consecutive short suites passed in 44.8–45.2s after this change.
The race suite passed in 58.9s. Compact load cells assert correctness,
commit progress and typed/fast rejections. Latency and memory trends are recorded.
Full mode adds the
wal-growth scenario and the wal-matrix (19 minutes on its own) and writes
`results/<date>.tsv`, `results/<date>-wal.tsv`,
`results/<date>-wal-matrix.tsv`, `results/<date>-load.tsv`, and `results/latest.md`.
The load scenario alone takes about 12 minutes. Only complete load coverage
replaces its record. A filtered run leaves the complete record intact. Database files go to
`.labdata/` next to the tests so they sit on a real disk. `LAB_DIR` can
move other scenarios, but load always uses `.labdata/` for the disk probe and databases.

## What this lab does not prove

- Nobody pulled the plug. The durability section is SQLite's documented
  behavior plus measured cost, not a crash test.
- The checkpoint fix for several writer connections was measured with four
  connections in one process, not across processes. Same lock, same code
  path, but E with that fix was not run.
- `Store.Checkpoint` is the newest code here. It was measured for 30 s at a
  time, with eight readers whose reads took 60 to 110 ms, on one machine.
- The matrix has one write rate and three read lengths (0.1, 0.6 and about
  60 ms). How long a read may get, or how slowly several connections must
  write, before the WAL stops starting over was not mapped.
- Litestream holds its own read transaction and wants to own checkpointing.
  How that interacts with the WAL rules above is untested here, and
  `/srv/prod` databases are litestreamed.
- The numbers were taken on a shared, busy machine. The ratios are large
  enough to survive that; the absolutes are not portable.
