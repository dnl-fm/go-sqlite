# database

SQLite WAL has one writer. This package makes that limit explicit: one permanent
writer connection and a separate read-only pool, using `modernc.org/sqlite`.
Writers wait in Go instead of racing inside SQLite's lock handler.

```go
db, err := database.Open(ctx, "app.db")
if err != nil {
    return err
}
defer db.Close()
```

`Query` and `QueryOne` use the reader. `Exec` and `BeginTx` use the writer;
`BeginTx` with `ReadOnly: true` uses the reader. `DB()` and `Writer()` return
the writer, so existing repositories and migrations can use them safely.
`Reader()` is enforced read-only by SQLite's `mode=ro`, even if a statement
turns `query_only` off.

`:memory:`, `file::memory:` and `mode=memory` URI paths share one permanent
`*sql.DB` connection for both roles. They ignore reader pool settings. Their
reader handle can write; SQLite cannot open a separate read-only in-memory
connection that shares this private database. Finish each transaction before
using another public helper on these paths.

## Read-only open

A process that does not own the file opens it with `OpenReadOnly`:

```go
db, err := database.OpenReadOnly(ctx, "/srv/prod/app/data/app.db")
```

It returns the same `*Database` with a reader pool only, sized by `MaxOpenConns`.
`Query`, `QueryOne`, `Read`, `Reader()` and `BeginTx` with `ReadOnly: true` work.
`Exec`, `Write`, any other `BeginTx`, `Checkpoint` and `Queue.Submit` return
`ErrReadOnly`. `DB()` and `Writer()` return the reader pool, where SQLite's
`mode=ro` rejects writes.

The file must exist and be in WAL mode; `OpenReadOnly` never creates or
switches it. A reader of a rollback-journal file would block the owner's
writes. In-memory paths are an error. Turso-format files are refused as in `Open`.

PRAGMA rule: `journal_mode` and `synchronous` belong to the owner and are never
sent. Every other configured PRAGMA is tried on a `mode=ro` connection. SQLite
rejects those that would write the file, such as `user_version = 7`. They are
omitted, or fail the open with `WithStrictPragmas(true)`.

## Custom SQL functions

```go
err := database.RegisterDeterministicScalarFunction("slug", 1,
    func(_ *database.FunctionContext, args []driver.Value) (driver.Value, error) {
        title, _ := args[0].(string)
        return strings.ToLower(title), nil
    })
```

`RegisterScalarFunction`, `RegisterDeterministicScalarFunction` and
`RegisterFunction` (with `FunctionImpl`, also for aggregates) wrap modernc's
registration. `FunctionContext`, `FunctionImpl` and `AggregateFunction` are
aliases of its types, so no modernc import is necessary. Registration is
process-global and a name registers once. Register before `Open` or
`OpenReadOnly`: every connection opened afterwards has the function, on both
pools. Connections that are already open never get it. Only a deterministic
function can appear in an index expression or a generated column.

## Configuration

`DefaultConfig`, `DevelopmentConfig` and `ProductionConfig` have the same defaults:

| Setting | Default | Applies to |
|---------|---------|------------|
| `MaxOpenConns` / `MaxIdleConns` | 4 / 4 | Reader pool |
| `ConnMaxLifetime` | 0 | Reader pool |
| `journal_mode` | WAL | Both pools; required for files |
| `synchronous` | NORMAL | Both pools |
| `busy_timeout` | 5000 ms | Both pools |
| `foreign_keys` | ON | Both pools |
| `temp_store` | MEMORY | Both pools |
| `cache_size` | -16000 | Both pools; about 16 MB per connection |

The writer always has one open connection, one idle connection and no lifetime
limit. Nonpositive `MaxOpenConns` means four readers. `Open` copies the config.

```go
cfg := database.DefaultConfig().
    WithMaxOpenConns(8).
    WithMaxIdleConns(8).
    WithPragma("synchronous", "FULL").
    WithStrictPragmas(true)
db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

`WithPragmaOrder` sets startup validation order; remaining keys follow in sorted
order. By default, `Open` omits rejected PRAGMAs. With `WithStrictPragmas(true)`,
`Open` fails startup instead. Every accepted setting travels in modernc's `_pragma` DSN
parameters and reaches every replacement connection. The driver applies
`busy_timeout` first, then sorts the others; it does not preserve a custom
per-connection order. Files must report `journal_mode=wal` after opening.

`NORMAL` survives application crashes but can lose recent commits after power
loss. Use `FULL` when acknowledged writes must survive power loss.

## Transactions and batching

```go
err := db.Write(ctx, func(tx *sql.Tx) error {
    var value int
    if err := tx.QueryRowContext(ctx, "SELECT value FROM counters WHERE id=1").Scan(&value); err != nil {
        return err
    }
    _, err := tx.ExecContext(ctx, "UPDATE counters SET value=? WHERE id=1", value+1)
    return err
})
```

`Write` starts `BEGIN IMMEDIATE` and commits on success. An error or panic
rolls back the transaction. The panic continues. Read-modify-write operations cannot lose an update
when every write uses this owner. `Read` runs a snapshot transaction and rolls
it back on return or panic. Never hold `Rows` or a transaction open across slow
work. Never call a writer helper inside a writer callback: its connection is
already occupied.

For sustained traffic, put the FIFO batching queue in front of the writer:

```go
queue := database.NewQueue(db, database.QueueOptions{})
defer queue.Close() // drain before closing db
err := queue.Submit(ctx, func(tx *sql.Tx) error {
    _, err := tx.Exec("INSERT INTO events(value) VALUES(?)", "created")
    return err
})
```

Defaults are 64 operations per batch and a 1 ms collection window. Each callback
has a savepoint: its error or panic rolls back only its own changes. A batch
failure rolls back all operations. `Submit` returns success only after commit.
Cancellation skips queued work; once work starts, `Submit` waits for its real
result. Use only the supplied transaction and no cancellable SQL context inside
a queue callback. `Close` stops admission and drains accepted operations.

## WAL and inspection

One process owns writes to each physical file. Long reads pin the WAL. Route
those reads through `Read` and call `Checkpoint(ctx, 50*time.Millisecond)` from
one timer when needed. The checkpoint takes the writer and pauses new `Read` calls. It waits up to
the quiet period for active reads. Then it truncates the WAL with the same
bounded lock wait. It restores the writer's previous timeout. A busy result
means a reader still pins the WAL; try again later. `Query`, `QueryOne`, direct
reader calls and external readers bypass the quiet window. A transaction that
never ends defeats every checkpoint.

Prefer the owner's API for live inspection. `sqlite3` can safely read SQLite WAL
files or owner-produced snapshots. Never copy only the database file while WAL
writes are active. Files in the older Turso format must be converted with
`cmd/turso-convert`. `Open` rejects their marker or log sidecar before SQLite
touches the file.

See [operations](../../docs/operations.md) for the supported load envelope and
[consumer guide](../../docs/consumer-guide.md) for upgrading existing callers.
The executable reference is [sqlite-writers](../../lab/sqlite-writers/README.md).
