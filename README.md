# go-turso-kit

Type-safe SQLite toolkit for Go with [Turso](https://turso.tech) support.

## Features

- **Named Parameter Queries** - Write SQL with `:name` placeholders
- **Repository Pattern** - Generic CRUD with automatic struct scanning
- **Migrations** - Schema versioning with up/down functions
- **Transactions** - Automatic commit/rollback handling
- **Zeit** - Timezone-aware datetime and billing cycles
- **ID Generation** - ULID (time-sortable) and NanoID (compact)

## Installation

```bash
go get github.com/fightbulc/go-turso-kit
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
    "database/sql"
    _ "turso.tech/database/tursogo"
    "github.com/fightbulc/go-turso-kit/pkg/repository"
)

db, _ := sql.Open("turso", ":memory:")
repo := repository.New[User, string](db, "users")
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
import "github.com/fightbulc/go-turso-kit/pkg/query"

// Build query with :name placeholders
q, err := query.Build(
    "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
    map[string]any{
        "id":    "user_123",
        "email": "alice@example.com",
        "name":  "Alice",
    },
)

// Execute
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
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, string]) error {
    // Insert user
    q1, _ := query.Build(
        "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
        map[string]any{"id": "1", "email": "alice@example.com", "name": "Alice"},
    )
    _, err := tx.Insert(ctx, q1)
    if err != nil {
        return err  // triggers rollback
    }

    // More operations...
    
    return nil  // triggers commit
})
```

### 6. Migrations

See [Migrations Guide](#migrations-guide) below for complete CLI and integration details.

```go
import "github.com/fightbulc/go-turso-kit/pkg/migrations"

// Run pending migrations on startup
migrations.Run(ctx, db)

// Check status
statuses, _ := migrations.Status(ctx, db)

// Rollback last migration
migrations.Rollback(ctx, db, "")
```

### 7. ID Generation

```go
import (
    "github.com/fightbulc/go-turso-kit/pkg/id/ulid"
    "github.com/fightbulc/go-turso-kit/pkg/id/nanoid"
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
import "github.com/fightbulc/go-turso-kit/pkg/zeit"

// Current time in timezone
tokyo, _ := time.LoadLocation("Asia/Tokyo")
z := zeit.Now(tokyo)

// Database storage (UTC Unix timestamp)
dbValue := z.ToDatabase()  // int64
restored := zeit.FromDatabase(dbValue, tokyo)

// Date arithmetic
tomorrow := z.AddDays(1)
nextWeek := z.AddDays(7)
nextBusinessDay := z.AddBusinessDays(1)  // skips weekends

// Billing cycles
cycles := z.Cycles(12, zeit.Monthly)
for _, period := range cycles {
    fmt.Printf("%s to %s\n", 
        period.StartsAt.Format("2006-01-02"),
        period.EndsAt.Format("2006-01-02"))
}
```

## Migrations Guide

The toolkit includes a `migrate` CLI for managing database schema migrations.

### Building the CLI

```bash
# From go-turso-kit repo
make build
# Creates: bin/migrate
```

### CLI Commands

```bash
# Set database connection
export DATABASE_URL="file:./app.db"

# Create new migration (auto-detects module from go.mod)
./bin/migrate create add_users_table
# Creates: migrations/20251210123456_add_users_table.go

# Run all pending migrations
./bin/migrate up

# Show migration status
./bin/migrate status

# Rollback last migration
./bin/migrate down

# Rollback to specific version
./bin/migrate down 20251107000001
```

### Makefile Commands

```bash
make build              # Build migrate CLI
make migrate-create name=add_posts_table  # Create new migration
make migrate-up         # Run pending migrations
make migrate-down       # Rollback last migration
make migrate-status     # Show status
```

### Migration File Structure

Generated migrations follow this pattern:

```go
package migrations

import (
    "context"
    "database/sql"

    "github.com/fightbulc/go-turso-kit/pkg/migrations"
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
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `)
    return err
}

func down20251210123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `DROP TABLE users`)
    return err
}
```

### Integrating into Your App

**1. Add the dependency:**

```bash
go get github.com/fightbulc/go-turso-kit
```

**2. Get the migrate CLI** (choose one):

```bash
# Option A: Build from source
git clone https://github.com/fightbulc/go-turso-kit.git
cd go-turso-kit && make build
cp bin/migrate /usr/local/bin/

# Option B: Go install (if available)
go install github.com/fightbulc/go-turso-kit/cmd/migrate@latest
```

**3. Create migrations directory and first migration:**

```bash
mkdir migrations
migrate create create_users_table
```

The CLI auto-detects your module name from `go.mod` and generates the correct import path.

**4. Import migrations in your app (blank import triggers registration):**

```go
package main

import (
    "context"
    "database/sql"
    "log"
    "os"

    "github.com/fightbulc/go-turso-kit/pkg/migrations"
    _ "turso.tech/database/tursogo"
    
    // Blank import registers all migrations via init()
    _ "yourapp/migrations"
)

func main() {
    db, err := sql.Open("turso", os.Getenv("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    // Run pending migrations on startup
    ctx := context.Background()
    if err := migrations.Run(ctx, db); err != nil {
        log.Fatal("migration failed:", err)
    }

    // ... rest of your app
}
```

### How It Works

1. Each migration file has an `init()` function that calls `migrations.Register()`
2. Blank importing the migrations package (`_ "yourapp/migrations"`) executes all `init()` functions
3. `migrations.Run()` checks the `_migrations` table and runs any pending migrations
4. Migrations are tracked by version (timestamp) in the `_migrations` table

### Programmatic API

```go
import "github.com/fightbulc/go-turso-kit/pkg/migrations"

// Run all pending migrations
err := migrations.Run(ctx, db)

// Get migration status
statuses, err := migrations.Status(ctx, db)
for _, s := range statuses {
    fmt.Printf("%s: %s (executed: %v)\n", s.Version, s.Description, s.ExecutedAt != nil)
}

// Rollback to specific version (or empty string for last)
err := migrations.Rollback(ctx, db, "20251107000001")

// Get latest executed version
version, err := migrations.Latest(ctx, db)
```

## Package Overview

| Package | Description |
|---------|-------------|
| `pkg/database` | Connection wrapper with pragmas and pooling |
| `pkg/query` | Named parameter query building |
| `pkg/repository` | Generic CRUD operations |
| `pkg/scan` | Automatic struct scanning from sql.Rows |
| `pkg/migrations` | Schema versioning |
| `pkg/id/ulid` | Time-sortable unique IDs |
| `pkg/id/nanoid` | Compact random IDs |
| `pkg/zeit` | Timezone handling and billing cycles |

## Examples

See [tmp/examples/](./tmp/examples/) for complete working examples:

```bash
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```

## Development

### Makefile Commands

```bash
make build          # Build migrate CLI to bin/
make test           # Run all tests
make test-cover     # Run tests with coverage
make test-verbose   # Run tests with verbose output
make clean          # Remove build artifacts
```

### Testing

```bash
# Run all tests
make test

# With coverage
make test-cover

# Verbose
make test-verbose
```

## Requirements

- Go 1.21+
- [tursogo](https://github.com/tursodatabase/turso/tree/main/bindings/go) driver (v0.4.4+)

## Driver Notes

The new `turso.tech/database/tursogo` driver (v0.4.4) supports additional DSN options:

```
:memory:?vfs=io_uring&async=1&_busy_timeout=5000
```

| Option | Values | Description |
|--------|--------|-------------|
| `vfs` | `memory`, `syscall`, `io_uring` | Virtual filesystem backend |
| `async` | `0`, `1` | Enable async I/O mode |
| `_busy_timeout` | milliseconds | Busy timeout for locked databases |

**⚠️ io_uring status (Feb 2026):** Benchmarks show `io_uring` is 20-40% slower than `syscall` and crashes under high concurrency. Stick with the default `syscall` VFS until the driver matures.

## License

MIT
