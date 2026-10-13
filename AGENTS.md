# github.com/dnl-fm/go-sqlite

## Stack

- Go
- SQLite through `modernc.org/sqlite v1.60.1` (pure Go)

## Components

| Name | Path | Type |
|---|---|---|
| migrate | `cmd/migrate/` | CLI |
| pkg | `pkg/` | Library |

## Commands

```bash
make test
make lab-test # modernc writer and tenant labs
make build # migrate CLI

# Historical raw Turso evaluation, separate modules
make lab-memory TURSO_VERSIONS="v0.7.2 v0.8.1"
```

## Consumer notes

One process owns each database file. Route every write through that process.
`database.Open` uses one writer connection and a separate read-only pool (four
connections by default). `DB()` and `Writer()` return the writer.
`Reader()` returns the reader pool. `Query` and `QueryOne` use the reader.
`Exec` and write transactions use the writer.

A process that does not own the file opens it with `OpenReadOnly`: reads only, write methods return `ErrReadOnly`, the file must exist in WAL mode.
Register custom SQL functions with `RegisterFunction` and its scalar variants before `Open` or `OpenReadOnly`; registration is process-global.

Use `Write` for multi-statement writes and read-modify-write transactions.
Use `Queue` for sustained traffic, FIFO writes, or group commits with
`synchronous=FULL`. Use only the supplied transaction inside each callback.
Never call `Write`, `Queue.Submit`, or `Checkpoint` from a write callback.

Close every `Rows` and transaction promptly. Open readers pin the WAL.
For long reads, use `Read` and one timer with `Checkpoint(ctx, quiet)`.
The checkpoint pauses new `Read` calls and returns `busy` if readers prevent
truncation. Direct `Reader()` queries bypass that pause. Choose a quiet window
that matches ordinary read duration. Inspect repeated busy results and WAL size.
Do not disable automatic checkpoints without an explicit checkpoint owner.

For `:memory:` and `file::memory:` databases, both roles share one connection.
Do not hold rows or a transaction while another operation needs that connection.
SQLite supports `WITHOUT ROWID` tables.

The supported envelope comes from [the writer lab](lab/sqlite-writers/README.md):

- One writer process per file.
- Up to 1,000 sustained writes/s per database.
- Bursts up to approximately 5,000 writes/s for a few seconds.
- A write deadline of approximately one second, then `context.DeadlineExceeded`.
- Fast overload rejection: HTTP 503 with `Retry-After` and client backoff with jitter.

Set deadlines and admission limits in the consumer. The package does not impose
a one-second deadline or implement HTTP responses. Keep load at or below half
of measured capacity for tight latency tails. Run the lab on the real workload
before relying on higher rates, larger transactions, or long reads.
Several writer processes per file are outside this envelope.

Prefer the owner's authenticated API or local admin socket for live inspection.
Plain `sqlite3` supports read-only inspection of snapshots and live SQLite WAL
databases. No special CLI is required. Never raw-copy an open database as a
snapshot. Use an owner-produced snapshot or SQLite's backup operation.

Before v0.8.0, convert Turso-format files with [cmd/turso-convert](cmd/turso-convert/).
Stop the owner first. Convert to a new file, verify it, then switch the service.
`Open` rejects a Turso MVCC header (bytes 18–19 are `0xff 0xff`) or a `-log`
sidecar and names the converter in its error.

The `lab/turso-*` modules and recorded results are historical upstream evidence.
They do not define the current library contract. Current proof lives in
`lab/sqlite-writers` and `lab/sqlite-tenants`. Read [the consumer guide](docs/consumer-guide.md)
and [operations](docs/operations.md) before consumer upgrades.
