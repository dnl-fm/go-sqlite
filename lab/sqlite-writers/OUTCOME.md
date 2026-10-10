# Outcome: one writer per file holds up — use the queue under load

Runs 2026-10-07 to 2026-10-10 on the devbox (ext4 on NVMe RAID1),
`modernc.org/sqlite` v1.60.1 / SQLite 3.53.4, Go 1.26.5. Full numbers:
[results/latest.md](results/latest.md). The README is the playbook; this is
what we learned.

## The short version

One process owns the database file and all writes go through one writer
connection. Do that and SQLite has no lock errors, no lost updates, a WAL
that stays small, and readers that never wait. Under sustained traffic put the
batching `Queue` in front of the writer, keep load at or below half of measured
capacity, give every write a deadline of about a second, and answer
`DeadlineExceeded` with "retry later". What we promise a project is in the
README's "What we support" section: up to 1,000 writes/s per database, which is
1–8% of what we measured.

## What each part proved

- **Several writer connections break things.** A shared pool with deferred
  transactions rejected 57% of read-modify-write increments
  (`SQLITE_BUSY`/`BUSY_SNAPSHOT`). `BEGIN IMMEDIATE` fixes correctness but
  writers stuck in SQLite's busy handler drag readers to 600–800 ms p99.
- **Separate processes are a last resort.** SQLite's cross-process lock wait
  is a polling retry, not a queue. Behind back-to-back long transactions a
  process lost every race until `busy_timeout` ran out: `SQLITE_BUSY` in 6 of
  9 runs, worst write 9.5 s. Route other processes' writes through the owner.
- **The WAL grows when it can never restart.** It restarts only at a moment
  when a checkpoint has copied everything and no reader is on an older
  snapshot. Several writer connections, reads that span commits (~60 ms
  scans), or one read transaction left open each remove that moment: 350–
  395 MB in 30 s instead of 4 MB, with 0 restarts. `journal_size_limit` does
  not help. `Store.Checkpoint` (a 50 ms read pause plus TRUNCATE on a timer)
  bounds the long-scan case at ~60 MB. Nothing bounds a read transaction that
  never ends.
- **Go's pool is not a queue.** `database/sql` hands a freed connection to a
  random waiter, so behind a 100 ms transaction some writers waited 806 ms.
  The `Queue` is FIFO: 115 ms.
- **Under load, the queue meets the contract and the plain pool doesn't.** At
  50% of capacity, the queue had zero errors, write p99 26–37 ms (within 3×
  the disk's fsync p99), max ≤ 95 ms, and drained 200% bursts in 4–6.5 s. The
  plain pool missed caller deadlines in bursts and at 80% because of the
  random waiter order. At 80% even the queue's tails stretched toward the
  deadline on a shared host — that is the headroom rule.
- **Overload is a clean, fast no.** At 150% load, writes past a 100 ms
  admission budget failed with `context.DeadlineExceeded` in ~115 ms; accepted
  writes stayed fast and memory stayed bounded.
- **The batching only pays with `synchronous=FULL`.** There it is ~55× the
  plain pool's throughput; under `NORMAL` it costs ~1 ms per write.

## Process notes

- Two independent builds of this lab (Opus 5.5 and Sol 6.1, same spec)
  disagreed about WAL growth. Both were right about their own setup: one ran on
  tmpfs, where fsync is free, and one counted rows with a full-table scan. The
  matrix on real disk settled it. Measure on the disk you deploy to.
- The load contract took three rounds because two of its numbers were set
  before measuring (a fixed 25 ms p99, a burst deadline shorter than its own
  backlog). The asserted contract is now disk-relative and arithmetic-checked.

## Still open

- A single quick-run failure on 2026-10-09 never recurred (20 passes since);
  its output was not captured.
- Litestream is untested against the WAL rules. It holds its own read
  transaction and wants to own checkpointing, and `/srv/prod` uses it.
- Crash and power-loss behaviour is SQLite's documented semantics, not
  measured here (the tenants lab covers SIGKILL).
- The read-length threshold where reads start blocking WAL restarts lies
  somewhere between 0.6 and 60 ms and is not mapped.
