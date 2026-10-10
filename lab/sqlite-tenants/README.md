# One SQLite database per tenant

SQLite lets one connection write at a time. Per file. Put every customer in
the same file and they all stand in the same queue: one customer's 100 ms
import transaction is 100 ms added to everybody's checkout.

Give each tenant their own file and the queue becomes per tenant. In this lab
a neighbor holding a 100 ms write transaction in a loop moved the other
tenants' write p99 to 0.09 ms, and 16 tenants took six times the writes of
one database with the same 64 writers (122,000 a second against 20,000). Deleting a customer is deleting a file.
Backing one up is copying a file.

You pay for it in three places: a query across tenants is now a loop, a
migration is now N migrations, and somebody has to decide how many files stay
open. This lab is that somebody: about 570 lines of pattern code, ten
scenarios that prove what it promises, and the numbers to size it.

```go
m, err := tenants.New(tenants.Config{
    Root:       "/srv/prod/app/data/tenants",
    MaxOpen:    256,
    Migrations: []string{schemaV1, schemaV2}, // append only
})

err = m.Create(ctx, "acme")

err = m.With(ctx, "acme", func(t *tenants.Tenant) error {
    return t.Write(ctx, func(tx *sql.Tx) error {
        _, err := tx.ExecContext(ctx, `INSERT INTO events (writer, seq) VALUES (?, ?)`, 1, 1)
        return err
    })
})
```

`<root>/acme.db` is the tenant. Each open tenant has one writer connection
(`BEGIN IMMEDIATE`, so writes queue instead of failing with `SQLITE_BUSY`) and
a pool of four `query_only` readers, all in WAL mode with
`synchronous=NORMAL`, `busy_timeout=5000`, `foreign_keys=ON`. That is the
`openSQLite` recipe from schubkraft, once per tenant.

## When this is the right call

Use it when the tenant is the unit of everything: its rows never join another
tenant's rows, you get asked to export or delete one customer, and one busy
customer must not slow the rest.

Don't use it for these three:

**Cross-tenant queries on the hot path.** "Top 10 customers by revenue" is a
fan-out over every file, merged in Go (`ForEach`). The tempting shortcut is
one connection with every tenant ATTACHed. SQLite stops you at ten:
`too many attached databases - max 10` (scenario 9). Fan-out is fine for a
nightly report. It is not fine for a dashboard that loads on every page view.
Keep a separate global database for anything you ask about all tenants at
once.

**Global uniqueness.** "This email is already registered" cannot be a
`UNIQUE` constraint, because no single database sees all the emails. Logins,
slugs, and the list of tenants itself belong in one small directory database
next to the tenant files.

**Tiny tenants in huge numbers.** An empty tenant costs 8 KB on disk, a cold
open costs 0.4 ms, and a schema change touches every file: 1,000 tenants
migrate in 2 s, so 100,000 take over three minutes, and your backup job now
ships 100,000 objects. If the typical tenant is twenty rows, a `tenant_id`
column in one database is the better design.

## Copying it

Copy `tenant.go` and `tenants.go`, rename the package, and
`go get modernc.org/sqlite@v1.60.1`. No other dependencies. Then:

1. Write your schema as `Config.Migrations`. Step *i* takes
   `PRAGMA user_version` from *i* to *i+1*.
2. Pick `MaxOpen` from the sizing rules below.
3. Call `MigrateAll` at boot, before taking traffic, and alert on every
   failed result.
4. Call `Close` on shutdown, after the HTTP server has drained.

One process owns the root directory. The cache, the single-writer rule, and
`Delete` all assume nobody else has these files open.

## What it guarantees

Every line here is a scenario in `tenants_test.go`. Run them before you trust
them.

| Guarantee | Proof |
|---|---|
| A tenant ID cannot leave the root. `../x`, `a/b`, `A`, `a.db`, and the empty string are rejected before any file call. | 01 |
| A typo never creates a tenant. Unknown ID is `ErrNotFound`, no file appears, and it does not evict anybody from a full cache. | 02, 03 |
| `Delete` turns new users away, waits for the ones inside `With`, then removes `.db`, `-wal`, `-shm`. | 02 |
| Never more than `MaxOpen` tenants open, counted by the manager and by the kernel (`/proc/self/fd`). A tenant in use is never evicted. | 03 |
| When every slot is in use, the next open waits for one and honors its context. | 03 |
| Writes to different tenants do not contend. No errors, exact row counts. | 03, 04 |
| A long write transaction in one tenant delays only that tenant. | 05 |
| A migration step is atomic. A tenant whose step fails stays at its old version, untouched, and is refused with a `*MigrationError` instead of served. | 06 |
| `Backup` of a tenant under write load is the database as of one commit, and passes `integrity_check`. | 07 |
| `SIGKILL` mid-write loses nothing that was committed and leaves nothing half-committed. | 08 |

`synchronous=NORMAL` is a deliberate trade. A killed process loses nothing:
committed transactions are in the WAL, and the WAL is in the kernel's page
cache. A power cut can lose the last few commits, though never corrupt the
file. If a tenant's last second of writes must survive a power cut, that is
`synchronous=FULL` and an fsync on every commit.

## Sizing: pick MaxOpen from your active tenants

An open tenant is cheap. Closing one that has been written to is not. Size
the cache so that tenants active in the same minute all fit.

Measured with 1,000 tenants on a 16-core box, ext4 on RAID1, each cache size
in a fresh process. Three other benchmarks shared the machine during the run,
so read the disk-bound numbers as pessimistic.

| Tenants open | RssAnon | Per tenant | File descriptors |
|---:|---:|---:|---:|
| 0 | 2.6 MB | | 7 |
| 10 | 7.2 MB | | 57 |
| 100 | 29.5 MB | 269 kB | 507 |
| 1,000 | 245.5 MB | 243 kB | 5,007 |

That is each tenant read once and written once: the writer plus one reader
connection. Ten tenants is too few to divide by; the Go heap moves more than
that.

**Memory.** Budget 0.25 MB per open tenant at rest. A connection's page cache
grows to SQLite's default 2 MB as it touches a larger database, so the
ceiling for a big, busy tenant is five connections times 2 MB, 10 MB.
Evicting gives the memory back: closing all 1,000 dropped RssAnon from
245.5 MB to 32.3 MB.

**File descriptors.** The writer holds three (`.db`, `-wal`, `-shm`) and each
reader two. One writer and four busy readers is 11:

```
ulimit -n  >=  11 x MaxOpen + what the rest of the process needs
```

The default `MaxOpen` of 64 needs 704, which fits a stock `ulimit -n` of
1024. `MaxOpen: 1000` needs 11,000, so raise the limit in the unit file
first. Running out of descriptors shows up as
`unable to open database file (14)` on whichever tenant asked next, usually
at the busiest moment.

**The miss.** What a request pays to get its tenant:

| | p50 | p99 |
|---|---:|---:|
| Tenant already open | 0.02 ms | 0.04 ms |
| Cold open, cache has room | 0.43 ms | 0.73 ms |
| Cold open, evicting a tenant that was only read | 0.34 ms | 0.54 ms |
| Cold open, evicting a tenant that was written | 10.20 ms | 25.98 ms |

One goroutine, open plus first query. The first two rows are the MaxOpen
1000 run, the last two MaxOpen 100.

The last row is the limit of this pattern. Closing the last connection to a
WAL database checkpoints it: fsync the WAL, copy the pages, fsync the
database, unlink the WAL. That is 10 ms of disk, paid by the request that
needed the slot. Sixteen workers doing it at once queue on the disk:

| 1,000 tenants, MaxOpen 64, 16 workers, 60 s | Hit rate | Requests/s | p50 | p99 |
|---|---:|---:|---:|---:|
| Every tenant equally likely | 6.3 % | 358 | 47.99 ms | 55.04 ms |
| A few hot tenants (Zipf, s=1.1) | 59.1 % | 655 | 0.25 ms | 72.27 ms |

A request is one read transaction and one write. With a hot set that mostly
fits, the median request never notices the cache. The tail is a miss in both
rows, and a miss costs the same whoever takes it.

So `MaxOpen` is a ceiling on memory and descriptors, not a knob to keep
small. At 0.25 MB a tenant, `MaxOpen: 2000` is half a gigabyte and 22,000
descriptors. Spend them. Watch `Stats()`: if `Evictions` climbs during normal
traffic, the cache is too small.

**Disk.** An empty tenant is two pages, 8 KB. While it is open add the WAL
(8 KB after the first migration, more as it is written) and a 32 KB `-shm`.
Creating one took 90 ms with 16 creators running, 179 tenants a second: the
same fsyncs again.

Idle-timeout eviction is not implemented. A tenant stays open until the LRU
needs its slot or the process exits.

## Migrations: fan out at boot, expect one bad tenant

Two paths bring a tenant to the current schema, and you want both.

`MigrateAll(ctx, concurrency)` at boot is the rollout. It opens every tenant,
which migrates the ones that are behind, and returns one result per tenant:

| 1,000 tenants, one step | Wall time |
|---|---:|
| concurrency 1 | 23.1 s |
| concurrency 8 | 7.2 s |
| concurrency 32 | 2.1 s |
| nothing to migrate, concurrency 32 | 0.14 s |

Use 32. The work is fsync-bound and the disk batches concurrent fsyncs, so it
scales well past the core count. When nothing is behind, the pass is a cheap
boot-time check that every tenant opens.

Migrate-on-open is the safety net. A tenant restored from last week's backup,
or one `MigrateAll` could not reach, migrates the first time `With` touches
it.

Now the 3am part. Somebody hot-fixed one tenant by hand in March, and your
`CREATE TABLE tags` collides with theirs. Scenario 6 does exactly that to one
tenant in twenty:

```
t0000 … t0019   MigrateAll   →   19 tenants at user_version 2, serving
t0007                        →   *MigrationError{Tenant: "t0007", Version: 1}
                                 still at user_version 1, the ALTER rolled back,
                                 rows intact, every With returns the error
```

One customer is down and nineteen never noticed. That is the trade: a tenant
that cannot migrate is refused, never served half-migrated. Page on a
`*MigrationError` from `MigrateAll`. Each `With` retries the migration, so
once you repair the tenant's schema it comes back on its next request, with no
restart.

The rules:

- Append steps. Never edit one that has shipped. Some tenant already ran it.
- One step is one transaction, so keep a step to what can roll back together.
- An old binary refuses a tenant whose version is newer than it knows. After
  `MigrateAll`, rolling back the binary takes every tenant down. Ship
  additive steps (new table, new nullable column) one deploy ahead of the
  code that needs them, and a rollback stays possible.
- `MigrateAll` walks the whole LRU. Run it before traffic, not during.

## Backups: per file, and N of them

`Backup(ctx, id, dst)` uses SQLite's online backup API (modernc exposes it as
`NewBackup`). It copies every page under one read snapshot, so the copy is
the tenant as of a single commit while writers keep writing. In scenario 7,
four writers ran throughout and each copy took 29 to 46 ms:

```
live rows before backup: 2676    copy: 2679    live rows after: 3123
```

The copy is a complete database with no WAL. Restore is: stop serving the
tenant, put the file at `<root>/<id>.db`, serve again. Migrate-on-open brings
an old copy up to the current schema.

Three things bite:

- **Never `cp` a live tenant.** The `.db` without its `-wal` is missing
  every commit since the last checkpoint. Use `Backup`.
- **Back up outside the root.** A file named `<something>.db` inside the root
  is a tenant.
- **N files means N of everything.** A nightly job is a `ForEach` that calls
  `Backup` and uploads, and it produces one object per tenant. Litestream is
  the wrong shape for this: it is configured per database and keeps a
  connection and a monitor on each one, while this pattern opens and closes
  thousands of files under an LRU and checkpoints on every eviction. This lab
  did not test them together. Until somebody does, per-tenant durability
  comes from scheduled `Backup`, and the recovery point is the schedule.

## Delete

`Delete` marks the tenant as going, and from that instant `With` returns
`ErrNotFound`. It waits for callers already inside `With`, closes the
handles, and removes `-wal`, `-shm`, then `.db`. The database goes last so a
crash in the middle leaves a valid tenant you can delete again, and never a
stale WAL lying in wait for the next tenant with the same ID.

If the context expires while it waits, nothing is deleted and the tenant
serves again. The ID is reusable as soon as `Delete` returns: `Create` gives
you an empty tenant.

Backups are not deleted. If "delete my data" has to mean the backups too,
that is your job.

## Where it breaks

- **Cache thrash.** Covered above: 50 ms requests when the active set does
  not fit `MaxOpen`.
- **`With` inside `With`.** Each holds a slot. Enough goroutines doing it on
  a full cache wait on each other until their contexts expire. Read from one
  tenant, return, then open the next.
- **Keeping `*Tenant` after `fn` returns.** It may be evicted and closed
  under you. The type is only valid inside the callback.
- **Holding a transaction open.** A write transaction blocks that tenant's
  other writers for its whole life (scenario 5: 100 ms held, 100 ms waited).
  A long read transaction pins that tenant's WAL: it cannot be checkpointed
  past the reader, so it grows until the reader finishes.
- **Two processes on one root.** Not supported. The single-writer rule is
  enforced by a Go connection pool, and a second process does not share it.

## Running it

```sh
cd lab/sqlite-tenants
GOWORK=off go vet ./...
GOWORK=off go test -count=1 ./...          # scenarios 1-9, about 20 s
GOWORK=off go test -race -count=1 ./...    # about 30 s
LAB_FULL=1 GOWORK=off go test -run Scenario -v -timeout 30m ./...   # adds scenario 10, about 4 min
```

The full run writes `results/<date>.tsv` and `results/latest.md`. Test
databases live in `.labdata-*` directories next to the code, not in `/tmp`,
because `/tmp` on the lab machine is tmpfs and would turn every fsync number
into a RAM number.
