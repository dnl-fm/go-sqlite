# go-sqlite

Type-safe SQLite toolkit for Go.

## Features

- **Database** - Connection wrapper with production PRAGMAs (WAL, foreign keys, busy timeout)
- **Named Parameter Queries** - SQL with `:name` placeholders
- **Repository Pattern** - Generic CRUD with automatic struct scanning
- **Migrations** - Schema versioning with up/down functions
- **Transactions** - Automatic commit/rollback handling
- **Zeit** - Timezone-aware datetime, unix storage, billing cycles
- **ID Generation** - ULID (time-sortable) and NanoID (compact)

## Installation

```bash
go get github.com/dnl-fm/go-sqlite
```

## Quick Start

### 1. Define Your Entity

Use `db` struct tags to map columns:

```go
type User struct {
    ID    string `db:"id"`
    Email string `db:"email"`
    Name  string `db:"name"`
}
```

### 2. Create Repository

```go
import (
    "context"
    "github.com/dnl-fm/go-sqlite/pkg/database"
    "github.com/dnl-fm/go-sqlite/pkg/repository"
    _ "github.com/dnl-fm/go-sqlite/pkg/driver/modernc"
)

db, _ := database.Open(ctx, "app.db")
defer db.Close()

repo := repository.New[User, string](db.DB(), "users")
```

### 3. CRUD Operations

```go
ctx := context.Background()

// Find by ID
user, err := repo.FindByID(ctx, "user_123")

// Find all
users, err := repo.FindAll(ctx)

// Count
count, err := repo.Count(ctx)

// Check existence
exists, err := repo.Exists(ctx, "user_123")
```

### 4. Named Parameter Queries

```go
import "github.com/dnl-fm/go-sqlite/pkg/query"

q, err := query.Build(
    "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
    map[string]any{
        "id":    "user_123",
        "email": "alice@example.com",
        "name":  "Alice",
    },
)
repo.Insert(ctx, q)

// Custom SELECT
q, _ := query.Build(
    "SELECT * FROM users WHERE email LIKE :pattern AND active = :active",
    map[string]any{"pattern": "%@example.com", "active": true},
)
users, err := repo.FindByQuery(ctx, q)
```

### 5. Transactions

```go
err := repo.WithTx(ctx, func(tx *repository.Repository[User, string]) error {
    q1, _ := query.Build(
        "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
        map[string]any{"id": "1", "email": "alice@example.com", "name": "Alice"},
    )
    _, err := tx.Insert(ctx, q1)
    if err != nil {
        return err  // triggers rollback
    }
    return nil  // triggers commit
})
```

### 6. Migrations

```go
import "github.com/dnl-fm/go-sqlite/pkg/migrations"

// Run pending migrations on startup
migrations.Run(ctx, db.DB())

// Check status
statuses, _ := migrations.Status(ctx, db.DB())

// Rollback last migration
migrations.Rollback(ctx, db.DB(), "")
```

### 7. ID Generation

```go
import (
    "github.com/dnl-fm/go-sqlite/pkg/id/ulid"
    "github.com/dnl-fm/go-sqlite/pkg/id/nanoid"
)

// ULID - time-sortable, 26 chars
id := ulid.New()                           // 01ARZ3NDEKTSV4RRFFQ69G5FAV
id := ulid.NewWithPrefix("user_")          // user_01ARZ3NDEKTSV4RRFFQ69G5FAV
ts := id.Time()                            // extract creation time

// NanoID - compact, 21 chars (configurable)
id := nanoid.New()                         // V1StGXR8_Z5jdHi6B-myT
id := nanoid.NewWithLength(10)             // shorter ID
```

### 8. Timezone-Aware Dates

```go
import "github.com/dnl-fm/zeit-go"

// Current time in timezone
tokyo, _ := time.LoadLocation("Asia/Tokyo")
z := zeit.Now(tokyo)

// Database storage (UTC Unix timestamp)
dbValue := z.ToDatabase()  // int64
restored := zeit.FromDatabase(dbValue, tokyo)

// Automatic via sql.Scanner/driver.Valuer (use *Zeit in structs)
type Order struct {
    ID        string     `db:"id"`
    CreatedAt *zeit.Zeit `db:"created_at"`  // scans as UTC int64
}
order, _ := repo.FindByID(ctx, "order_123")
order.CreatedAt.In(appTZ).ToUser()  // "2024-01-15T11:30:00+01:00"

// Date arithmetic
tomorrow := z.AddDays(1)
nextBusinessDay := z.AddBusinessDays(1)  // skips weekends

// Billing cycles
cycles := z.Cycles(12, zeit.Monthly)
```

## Database Configuration

`database.Open` applies production-ready PRAGMAs by default:

| PRAGMA | Default | Purpose |
|--------|---------|---------|
| `journal_mode` | `WAL` | Readers don't block writer |
| `synchronous` | `NORMAL` | Balanced durability/speed |
| `foreign_keys` | `ON` | Enforce referential integrity |
| `busy_timeout` | `5000` | Wait 5s before locked error |
| `temp_store` | `MEMORY` | Temp tables in RAM |
| `cache_size` | `-20000` | 20MB page cache |
| `mmap_size` | `33554432` | 32MB memory-mapped I/O |

```go
// Default config (recommended)
db, _ := database.Open(ctx, "app.db")

// Production config (larger cache, longer timeout)
db, _ := database.Open(ctx, "app.db",
    database.WithConfig(database.ProductionConfig()),
)

// Custom pragmas
cfg := database.DefaultConfig().WithPragma("cache_size", "-64000")
db, _ := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

## Migrations CLI

```bash
# Build
make build  # creates bin/migrate

# Usage
export DATABASE_URL="file:./app.db"
./bin/migrate create add_users_table
./bin/migrate up
./bin/migrate status
./bin/migrate down
```

Migration files register via `init()`:

```go
package migrations

import (
    "context"
    "database/sql"
    "github.com/dnl-fm/go-sqlite/pkg/migrations"
)

func init() {
    migrations.Register(migrations.Migration{
        Version:     "20251210123456",
        Description: "add_users_table",
        Up:          up20251210123456,
        Down:        down20251210123456,
    })
}

func up20251210123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        CREATE TABLE users (
            id TEXT PRIMARY KEY,
            email TEXT UNIQUE NOT NULL,
            created_at INTEGER NOT NULL DEFAULT 0
        )
    `)
    return err
}

func down20251210123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `DROP TABLE users`)
    return err
}
```

## Package Overview

| Package | Description |
|---------|-------------|
| `pkg/database` | Connection wrapper with PRAGMAs and pooling |
| `pkg/query` | Named parameter query building |
| `pkg/repository` | Generic CRUD operations |
| `pkg/scan` | Automatic struct scanning from sql.Rows |
| `pkg/migrations` | Schema versioning |
| `pkg/id/ulid` | Time-sortable unique IDs |
| `pkg/id/nanoid` | Compact random IDs |
| [`zeit-go`](https://github.com/dnl-fm/zeit-go) | Timezone handling and billing cycles (external) |
| `pkg/driver/modernc` | modernc.org/sqlite driver |

## Examples

```bash
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```

## Requirements

- Go 1.22+

## License

MIT
