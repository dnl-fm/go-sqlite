# Consumer guide

SQLite permits one writer per file. v0.8.0 makes that rule explicit: one
process owns the file, one connection writes, and a separate read-only pool reads.
The engine is `modernc.org/sqlite v1.60.1`. No driver choice is necessary.

Use `database.Open`, then `Write` for short transactions. Use `Read` for a
consistent read snapshot. Both callbacks end their transaction on return or
panic. `Write` commits a nil result and rolls back an error. Panics propagate.
Use only the supplied transaction inside each callback.

`DB()` and `Writer()` return the writer. `Reader()` returns the reader pool.
`Query` and `QueryOne` use the reader. `Exec` and `BeginTx` use the writer,
unless `BeginTx` receives `ReadOnly: true`. Repositories retain the handle you
supply. Use the reader for read repositories and the writer for write repositories.

File readers enforce read-only mode below the SQL layer. In-memory databases
share one connection between both roles, so writes remain visible to reads.
SQLite supports `WITHOUT ROWID` tables without an experimental flag.

## What we support

These limits come from [the writer lab](../lab/sqlite-writers/README.md#what-we-support),
not its measured ceilings:

| Item | Supported envelope |
|---|---|
| Writers | One process owns the file. All writes go through it. |
| Sustained writes | Up to 1,000/s per database. |
| Bursts | Up to approximately 5,000/s for a few seconds. |
| Write wait | At most approximately one second, then `context.DeadlineExceeded`. |
| Overload | Fast rejection: HTTP 503 with `Retry-After` and client backoff with jitter. |

The consumer supplies deadlines, admission limits, and HTTP responses.
Keep load at or below half of measured capacity for tight latency tails.
Use `Queue` for sustained traffic, FIFO writes, or group commits with
`synchronous=FULL`. The queue uses savepoints to isolate each operation.
Queued cancellation prevents execution. After an operation starts, `Submit`
waits for the actual result. Close the queue before the database to drain it.

Before relying on behavior outside this envelope, run the load scenario with
the real workload. This includes rates above 1,000/s, transactions larger than
a few small rows, several writer processes, long reads, or stronger durability promises.
Use another database for tens of thousands of writes/s or writers across machines.
Do the same for a promise to survive loss of the host.

`NORMAL` preserves acknowledged commits after an application crash.
Recent commits can be lost after an OS crash or power loss.
`FULL` fsyncs each commit. Use it when external actions depend on the acknowledgement.
The lab measured its cost. It did not pull the plug.

Close every `Rows` and transaction promptly. Open read snapshots pin the WAL.
For long reads, use `Read` and one timer with `Checkpoint(ctx, quiet)`.
The quiet window holds new `Read` calls back. Direct reader calls bypass it.
Repeated busy checkpoints require investigation of readers and WAL growth.
See [operations](operations.md) for checkpoint and inspection rules.

## Upgrading from v0.7.x

v0.8.0 removes the old driver and its APIs. There are no compatibility shims.

| v0.7.x API or dependency | v0.8.0 replacement |
|---|---|
| `ConcurrentTx`, `ConcurrentTxRetry`, database methods with those names | `db.Write(ctx, fn)` |
| `BeginConcurrentTx`, `ConcurrentTransaction` | `db.Write` or `db.BeginTx` on the writer |
| `IsRetryableConcurrentTxError` | Remove optimistic-conflict retry logic. Writers serialize in Go. |
| `WithTursoMVCC`, `TursoMVCCConfig` | Remove the option. Use `DefaultConfig` if explicit config is necessary. |
| `WithMultiprocessWAL`, `EnableMultiprocessWAL` | Remove. Keep one writer process per file. |
| `WithDriver`, `Config.Driver`, `DefaultDriver` | Remove. The driver is fixed. |
| `ConnTx` | `*sql.Tx` |
| `repository.WithConcurrentTx`, `WithConcurrentTxRetry` | `repository.WithTx` on a writer repository |
| Blank imports of `tursogo` or `pkg/driver/turso` | Remove. `pkg/database` registers modernc. |
| `ValidateRowIDSchema`, rowid-only checks | Remove. SQLite supports `WITHOUT ROWID`. |
| MVCC threshold PRAGMAs and experimental DSN flags | Remove. Use the WAL defaults. |

A retry callback becomes an ordinary write callback:

```go
err := db.Write(ctx, func(tx *sql.Tx) error {
    _, err := tx.ExecContext(ctx, "UPDATE counters SET value = value + 1 WHERE id = ?", id)
    return err
})
```

Do not call the outer database or submit another write inside this callback.
The callback holds the only writer connection.

### Convert deployed data

Turso-format files require conversion before v0.8.0 opens them.
`Open` rejects an MVCC header (bytes 18–19 are `0xff 0xff`) or a `-log`
sidecar and names `cmd/turso-convert`. Removing the sidecar or patching a header
alone does not establish a valid SQLite database.

1. Build the separate converter module:

   ```bash
   cd cmd/turso-convert
   GOWORK=off go build -o /tmp/turso-convert .
   ```

2. Stop the owning service cleanly.
3. Preserve the original database and all sidecars as the rollback source.
4. Convert into a new, absent destination:

   ```bash
   /tmp/turso-convert --check /path/to/app.db
   /tmp/turso-convert /path/to/app.db /path/to/app-sqlite.db
   sqlite3 -readonly /path/to/app-sqlite.db 'PRAGMA integrity_check;'
   ```

5. Review the converter report for schema and per-table row-count agreement.
6. Switch the service to the converted file with its normal deployment procedure.
7. Start v0.8.0 and run the app's test gate and authenticated health/read commands.

The converter works on a copy and preserves the source. Its output is a plain
SQLite WAL database. Read [the converter documentation](../cmd/turso-convert/)
for source-use detection limits. Do not treat a copied live file as a snapshot.

The `lab/turso-*` modules remain as historical evidence. Current library proof
lives in `lab/sqlite-writers` and `lab/sqlite-tenants`.
