# Operations

One process owns each database file. All writes use its single writer
connection. Readers use a separate read-only pool, so they do not wait behind
Go callers that need the writer. Several writer processes per file are outside
the [supported envelope](consumer-guide.md#what-we-support).

## Live inspection

Prefer named operations through the owner's authenticated API or local admin
socket. The app preserves permissions and business meaning:

```text
appctl customers get customer_01j...
    -> authenticated API or socket
    -> owning app -> reader pool -> app.db
```

This route is a consumer pattern. The library supplies no API or socket.
Each service documents its real commands, authentication, pagination, and response shape.
Generic inspection requires read-only enforcement, query deadlines, row limits,
and audit records. Writes go through business operations.

Plain SQLite tools can safely read a live WAL database in read-only mode:

```bash
sqlite3 -readonly app.db 'SELECT COUNT(*) FROM users;'
```

No special CLI is required. Such reads bypass the app's permissions and can
pin WAL snapshots. Keep them short. Prefer the owner's API for routine inspection.

## Snapshots and verification

Use an owner-produced snapshot, SQLite's backup operation, or a cleanly closed
database. A raw copy of an open database can omit WAL commits.
The library supplies no snapshot endpoint. Consumers own and test that operation.

```bash
sqlite3 -readonly snapshot.db 'PRAGMA integrity_check;'
sqlite3 -readonly snapshot.db 'SELECT COUNT(*) FROM users;'
```

Verify schema and relevant counts alongside integrity. Integrity alone does not
prove that an export contains the expected rows.

## WAL and checkpoints

An open `Rows` or read transaction pins a snapshot. If reads span commits
continuously, checkpoints cannot restart the WAL. A leaked reader can keep it
growing without limit. `journal_size_limit` only trims a WAL after a restart.

Use `Read` for long reads and one checkpoint timer per database:

```go
busy, err := db.Checkpoint(ctx, 50*time.Millisecond)
if err != nil || busy {
    log.Printf("checkpoint: busy=%t err=%v", busy, err)
}
```

Stop the timer before `db.Close`. Choose `quiet` from ordinary read duration.
The checkpoint reserves the writer, pauses new `Read` calls, and lets active
reads finish. It then tries `wal_checkpoint(TRUNCATE)` with a bounded lock wait.
Direct `Reader()` queries and external readers bypass the quiet window.
`busy` means a reader prevented truncation. Repeated busy results and growing
WAL size require investigation of open rows and transactions.

Do not call `Checkpoint` inside `Write` or a queue callback. That callback
already holds the writer connection. Do not add a plain blocking TRUNCATE
checkpoint timer for long reads. Its lock wait can stall writes for `busy_timeout`.
Do not disable automatic checkpoints without an explicit checkpoint owner.
Backup tools can hold their own read snapshots. The writer lab does not prove
checkpoint interaction with Litestream.

## Write pressure and shutdown

Use short transactions and a write deadline near one second. Reject overload
quickly with HTTP 503, `Retry-After`, and client backoff with jitter.
These controls belong in the consumer. Use `Queue` for sustained traffic or
FIFO service. Measure the real workload before exceeding the lab's envelope.

Use `synchronous=FULL` when an acknowledged commit must survive power loss.
`NORMAL` preserves commits after application crashes but can lose recent commits
after an OS crash or power loss. Queue batching shares the fsync cost under FULL.

Stop new requests and checkpoint timers first. Close the queue to drain accepted
operations. Then close the database. `Close` closes the reader pool before the
writer, which permits SQLite's final checkpoint.

## Existing storage

Before the new service opens a Turso-format database, convert it with
[cmd/turso-convert](../cmd/turso-convert/). Stop the owner and retain the
source for rollback. Follow [the upgrade procedure](consumer-guide.md#convert-deployed-data).
The old `lab/turso-*` modules are historical evidence, not an operating mode.
