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
