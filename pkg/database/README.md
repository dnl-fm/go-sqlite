# database

Database connection management for SQLite and Turso with context support.

## Overview

The `database` package provides a thin wrapper over Go's `database/sql` with configuration-based initialization and context propagation.

## Features

- Config-based initialization
- SQLite3 and Turso driver support
- Context propagation for cancellation
- Connection pooling
- Type-safe error handling

## Installation

```bash
go get github.com/fightbulc/go-turso-kit/pkg/database
```

## Quick Start

### Local SQLite

```go
import "github.com/fightbulc/go-turso-kit/pkg/database"

ctx := context.Background()

db, err := database.New(ctx, database.Config{
    Driver: "sqlite3",
    Source: "file:./mydb.sqlite",
})
if err != nil {
    log.Fatal(err)
}
defer db.Close()
```

### In-Memory Database (Testing)

```go
db, err := database.New(ctx, database.Config{
    Driver: "sqlite3",
    Source: ":memory:",
})
```

### Turso (Remote)

```go
db, err := database.New(ctx, database.Config{
    Driver: "turso",
    Source: "libsql://your-db.turso.io?authToken=your-token",
})
```

## Configuration

### Config Struct

```go
type Config struct {
    Driver string  // "sqlite3" or "turso"
    Source string  // Connection string
}
```

### Source String Formats

**SQLite (file):**
```
file:./path/to/db.sqlite
file:./db.sqlite?cache=shared&mode=rwc
```

**SQLite (in-memory):**
```
:memory:
file::memory:?cache=shared
```

**Turso:**
```
libsql://your-db.turso.io?authToken=your-token
```

## API Reference

### New

Creates and initializes a new database connection.

```go
func New(ctx context.Context, cfg Config) (*Database, error)
```

**Returns:**
- `*Database` - Database wrapper
- `error` - Configuration or connection error

**Errors:**
- `ErrInvalidConfig` - Invalid driver or empty source
- `ErrConnection` - Failed to connect or ping

### Database Methods

#### DB()

Returns underlying `*sql.DB` for advanced usage.

```go
func (d *Database) DB() *sql.DB
```

**Example:**
```go
stmt, err := db.DB().PrepareContext(ctx, "SELECT * FROM users WHERE id = ?")
```

#### Exec

Executes a query that doesn't return rows.

```go
func (d *Database) Exec(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
```

**Example:**
```go
result, err := db.Exec(ctx, "INSERT INTO users (id, email) VALUES (?, ?)", "123", "alice@example.com")
if err != nil {
    return err
}

rowsAffected, _ := result.RowsAffected()
fmt.Printf("Inserted %d rows\n", rowsAffected)
```

#### Query

Executes a query that returns rows.

```go
func (d *Database) Query(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
```

**Example:**
```go
rows, err := db.Query(ctx, "SELECT id, email FROM users WHERE active = ?", true)
if err != nil {
    return err
}
defer rows.Close()

for rows.Next() {
    var id, email string
    rows.Scan(&id, &email)
    fmt.Printf("%s: %s\n", id, email)
}
```

#### QueryRow

Executes a query that returns at most one row.

```go
func (d *Database) QueryRow(ctx context.Context, query string, args ...interface{}) *sql.Row
```

**Example:**
```go
var email string
err := db.QueryRow(ctx, "SELECT email FROM users WHERE id = ?", "123").Scan(&email)
if err == sql.ErrNoRows {
    // Not found
}
```

#### Close

Closes the database connection.

```go
func (d *Database) Close() error
```

**Example:**
```go
defer db.Close()
```

## Connection Pooling

Configure connection pool using underlying `*sql.DB`:

```go
db.DB().SetMaxOpenConns(25)
db.DB().SetMaxIdleConns(5)
db.DB().SetConnMaxLifetime(5 * time.Minute)
db.DB().SetConnMaxIdleTime(1 * time.Minute)
```

**Recommendations:**
- **Local SQLite:** MaxOpenConns=1 (SQLite doesn't support concurrent writes)
- **Turso:** MaxOpenConns=10-25 (HTTP-based connections)

## Error Handling

### Standard Errors

```go
var ErrInvalidConfig = errors.New("invalid database config")
var ErrConnection = errors.New("failed to connect to database")
```

**Usage:**
```go
db, err := database.New(ctx, cfg)
if err != nil {
    if errors.Is(err, database.ErrInvalidConfig) {
        // Handle config error
    }
    if errors.Is(err, database.ErrConnection) {
        // Handle connection error
    }
}
```

### Query Errors

```go
import "database/sql"

rows, err := db.Query(ctx, sql)
if err != nil {
    return fmt.Errorf("query failed: %w", err)
}

var email string
err = row.Scan(&email)
if err == sql.ErrNoRows {
    // Handle not found
}
```

## Context Usage

All operations accept `context.Context` for:
- Cancellation
- Timeouts
- Request-scoped values

**Example with timeout:**
```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

rows, err := db.Query(ctx, "SELECT * FROM large_table")
// Query will be cancelled after 5 seconds
```

**Example with cancellation:**
```go
ctx, cancel := context.WithCancel(context.Background())

go func() {
    time.Sleep(1 * time.Second)
    cancel()  // Cancel after 1 second
}()

rows, err := db.Query(ctx, "SELECT * FROM users")
// Query may be cancelled
```

## Best Practices

1. **Always use context** - Pass context to all operations
2. **Close rows** - Use `defer rows.Close()`
3. **Handle errors** - Check all error returns
4. **Use connection pooling** - Configure for your workload
5. **Prepared statements** - For repeated queries
6. **Transactions** - Use for atomic operations

## Examples

See [examples/basic/main.go](../../examples/basic/main.go) for complete working examples.

## See Also

- [query](../query/README.md) - Query builder
- [repository](../repository/README.md) - Repository pattern
- [migrations](../migrations/README.md) - Migration system
