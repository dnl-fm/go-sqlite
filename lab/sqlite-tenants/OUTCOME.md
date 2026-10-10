# Outcome: one SQLite file per tenant is cheap — size the cache to the active set

Runs 2026-10-07 on the devbox (ext4 on NVMe RAID1), `modernc.org/sqlite`
v1.60.1, Go 1.26.5. Full numbers: [results/latest.md](results/latest.md). The
README is the playbook; this is what we learned.

## The short version

A tenant manager that opens tenant files lazily, keeps at most `MaxOpen` of
them open, and never evicts one that is in use works: under `-race` with 16
goroutines over 50 tenants and a cap of 8, every write landed and the kernel
never saw more than 8 tenant files open. Open tenants are cheap (about
0.25 MB and up to 11 file descriptors each). Evicting a tenant that has been
written is not cheap: it costs a checkpoint and an fsync on the request that
needed the slot. So pick `MaxOpen` to cover the tenants that are active at the
same time, not the total.

## What each part proved

- **Isolation scales.** 16 tenants × 4 writers did 122,000 writes/s against
  20,000 for the same writes into one database (6×). A tenant holding 100 ms
  write transactions did not move other tenants' p99 (0.09 ms).
- **Cost per open tenant:** ~230–270 kB RSS and 5 fds idle, 11 fds with all
  four readers busy. 1,000 open tenants ≈ 245 MB and 5,000–11,000 fds — fine for
  memory, check `ulimit -n`.
- **Cold open is fast, dirty eviction is not.** Open plus first query
  0.4 ms; evicting a written tenant 10–26 ms. With uniform access over 1,000
  tenants and a 64-slot cache, requests sat at ~48 ms p50 (6% hits). With
  realistic skew (Zipf 1.1) hits rose to 59% and p50 to 0.25 ms.
- **Migrations isolate failures.** 19 of 20 tenants moved to v2; the poisoned
  one stayed at v1 with its failed step fully rolled back and is refused with
  a typed `*MigrationError` until repaired. Fan-out over 1,000 tenants: 23 s
  at concurrency 1, 2 s at 32.
- **Backups are consistent under writes.** The online backup API copies a
  snapshot at a commit boundary while four writers run; integrity check ok.
- **A killed process loses nothing it acknowledged.** SIGKILL after 300
  acknowledged commits: 300 present on reopen, no torn transactions.
- **ATTACH is not the cross-tenant tool.** It stops at 10 databases. Fan out
  with `ForEach` instead.

## Process notes

- Two independent builds of this lab (Opus 5.5 and Sol 6.1, same spec). This
  is the Opus build; it measured on real disk and found the dirty-eviction
  cost. Sol's build ran on tmpfs, but its eviction design was cleaner: the
  victim stays in the cache map until its handles are closed, so the same file
  can never be opened twice while it checkpoints. That design was ported here.

## Still open

- Eviction cost sits on the request path. Closing victims asynchronously
  would hide it but break the hard cap; not done.
- Waiters are woken by broadcast, not in order; under sustained slot
  starvation one can lose repeatedly until its deadline.
- `Close()` does not wait for in-flight `With` calls (documented precondition).
- Litestream with thousands of tenant files is untested; the README suggests
  scheduled `Backup` fan-out instead.
- One process per tenant root is assumed, not enforced.
