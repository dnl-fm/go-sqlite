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

For local file databases, `database.Open` also enables Turso's
`experimental=multiprocess_wal` DSN flag by default:

```text
app.db
app.db?experimental=multiprocess_wal
```

Existing query parameters are preserved:

```text
app.db?_busy_timeout=5000
app.db?_busy_timeout=5000&experimental=multiprocess_wal
```

In-memory databases and remote URLs are left alone.

## Live Inspection

Install `tursodb` on machines that run or inspect Turso-backed files. Use it
instead of system `sqlite3`.

```bash
tursodb app.db "PRAGMA integrity_check;"
tursodb --experimental-multiprocess-wal app.db ".tables"
```

The app-side flag is on by default for local file DBs, but the CLI still needs
`--experimental-multiprocess-wal` when the app may have the file open.

This is an inspection feature. It does not mean several Go application
processes can safely write the same DB file.

## Configuration

```go
cfg := database.DefaultConfig().
	WithMaxOpenConns(32).
	WithMaxIdleConns(16).
	WithPragma("busy_timeout", "5000")

db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

Opt out of the live-inspection DSN flag only for tests or labs that need to
prove the boundary:

```go
cfg := database.DefaultConfig().WithMultiprocessWAL(false)
db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
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
- [Turso consumer guide](../../docs/turso-060-consumer-guide.md)
- [query](../query/README.md)
- [repository](../repository/README.md)
- [migrations](../migrations/README.md)
