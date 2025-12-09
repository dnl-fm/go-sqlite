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
    _ "github.com/tursodatabase/turso-go"
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

```go
import "github.com/fightbulc/go-turso-kit/pkg/migrations"

// Register migrations
migrations.Register(migrations.Migration{
    Version:     "20251107000001",
    Description: "create_users_table",
    Up: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `
            CREATE TABLE users (
                id TEXT PRIMARY KEY,
                email TEXT UNIQUE NOT NULL,
                name TEXT NOT NULL,
                created_at DATETIME DEFAULT CURRENT_TIMESTAMP
            )
        `)
        return err
    },
    Down: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS users`)
        return err
    },
})

// Run pending migrations
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

## Testing

```bash
# Run all tests
go test ./pkg/...

# With coverage
go test ./pkg/... -cover

# Verbose
go test ./pkg/... -v
```

## Requirements

- Go 1.21+
- [turso-go](https://github.com/tursodatabase/turso-go) driver

## License

MIT
