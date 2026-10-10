# database

Connection-local PRAGMAs must reach every connection, or pooled queries can
lose foreign-key enforcement and use different timeouts.

The `database` package configures each native Turso connection before the pool
uses it, including connections created later. It validates rowid-table schema
and exposes context-aware query helpers.

Use this package instead of opening SQLite directly in Go services.

## Default Contract

```go
db, err := database.Open(ctx, "app.db")
if err != nil {
	return err
}
defer db.Close()
```

`database.Open`, `DefaultConfig`, `ProductionConfig`, and `DevelopmentConfig`
use Turso MVCC. There is no supported old-WAL or modernc path in this package.

Turso 0.7 makes MVCC and multiprocess WAL mutually exclusive. This package
chooses MVCC and one owning process per physical database file.

## Inspection

Live inspection goes through the owning service's authenticated API or local
admin socket. The library does not implement that service route; consuming apps
must expose the named reads their operators and agents need.

Install `tursodb` on machines that inspect offline Turso-backed files. Use it
instead of system `sqlite3`, and only against an owner-produced snapshot or a
cleanly closed database.

```bash
tursodb snapshot.db "PRAGMA integrity_check;"
tursodb snapshot.db ".tables"
```

Never raw-copy a live Turso database and call the result evidence.

## Configuration

```go
cfg := database.DefaultConfig().
	WithMaxOpenConns(32).
	WithMaxIdleConns(16).
	WithPragma("busy_timeout", "5000")

db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

`EnableMultiprocessWAL` and `WithMultiprocessWAL` remain only for source
compatibility. Enabling multiprocess WAL is rejected when `Open` applies the
package's MVCC contract:

```go
cfg := database.DefaultConfig().WithMultiprocessWAL(true)
db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
// err: multiprocess WAL is incompatible with go-sqlite's MVCC mode
```

`WithDriver` is kept for source compatibility, but this package always uses the
Turso driver and MVCC journal mode.

Each connection receives all configured PRAGMAs in `PragmaOrder`, followed by
the remaining keys in sorted order. Every connection must report `journal_mode`
as `mvcc`. With `StrictPragmas`, any PRAGMA error closes the new connection and
fails the operation that requested it. Without it, other PRAGMA errors are ignored.

## Memory

Turso MVCC keeps row versions in native memory until its GC and checkpoint
reclaim them. At Turso's own thresholds a steady write stream sat at 313–372 MiB
in the lab; the package defaults (`mvcc_gc_threshold = 1024`,
`mvcc_checkpoint_threshold = 262144`) hold the same workload at 171–192 MiB with
no measurable throughput cost. Don't go lower than that: GC on every commit
was 17–20× slower.

The native library uses glibc `malloc`, which keeps freed memory in per-thread
arenas. Run long-lived Turso services with `MALLOC_ARENA_MAX=2`; together with
the defaults above, the lab workload dropped to 66–81 MiB. Evidence and the
reproduction: `lab/turso-memory/OUTCOME.md`, `make lab-memory`.

## Queries

```go
rows, err := db.Query(ctx, "SELECT id, email FROM users WHERE active = ?", true)
if err != nil {
	return err
}
defer rows.Close()
```

Never hold `Rows` or a `Tx` open across other work. An open reader pins its MVCC
snapshot and prevents collection of old row versions. Close `Rows` promptly,
including after partial reads. Commit or roll back each transaction promptly.

```go
var email string
err := db.QueryOne(ctx, "SELECT email FROM users WHERE id = ?", "user_123").Scan(&email)
if errors.Is(err, sql.ErrNoRows) {
	return nil
}
```

```go
_, err := db.Exec(ctx, "INSERT INTO users (id, email) VALUES (?, ?)", "user_123", "a@example.com")
```

`Exec` rejects `WITHOUT ROWID` DDL because Turso MVCC cannot write those tables.
If a plain SQLite database already contains `WITHOUT ROWID`, rebuild it into
normal rowid tables before opening it with this package.

## Write Concurrency

One Go process with a pooled `*sql.DB` is the supported shape. For
multi-statement concurrent writes, use `ConcurrentTxRetry` or `BEGIN CONCURRENT`
on one reserved connection.

Several Go processes writing the same DB file is not a `go-sqlite` promise. If
you need multiple application writers, split the write domain or move writes
behind one owner process.

## See Also

- [Turso operations](../../docs/turso-operations.md)
- [Turso consumer guide](../../docs/turso-consumer-guide.md)
- [query](../query/README.md)
- [repository](../repository/README.md)
- [migrations](../migrations/README.md)
