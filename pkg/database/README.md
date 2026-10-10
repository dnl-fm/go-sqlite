# database

The `database` package is the `go-sqlite` open path. It opens through the
Turso `database/sql` driver, applies the package PRAGMAs, validates rowid-table
schema, and exposes context-aware query helpers.

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

## Queries

```go
rows, err := db.Query(ctx, "SELECT id, email FROM users WHERE active = ?", true)
if err != nil {
	return err
}
defer rows.Close()
```

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
