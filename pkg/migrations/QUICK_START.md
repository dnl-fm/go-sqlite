# Migration System - Quick Start

## 60-Second Setup

### 1. Create Migration
```bash
export DATABASE_URL="file:./app.db"
migrate create add_users_table
```

### 2. Edit Generated File
```go
// migrations/20251107123456_add_users_table.go
func up20251107123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        CREATE TABLE users (
            id TEXT PRIMARY KEY,
            email TEXT NOT NULL UNIQUE,
            name TEXT NOT NULL
        )
    `)
    return err
}

func down20251107123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `DROP TABLE users`)
    return err
}
```

### 3. Run Migration
```bash
migrate up
```

### 4. Check Status
```bash
migrate status
```

## Common Commands

```bash
# Create new migration
migrate create <name>

# Run all pending
migrate up

# Show status
migrate status

# Rollback last
migrate down

# Rollback to version
migrate down 20251107123456
```

## Programmatic Usage

```go
import (
    _ "github.com/fightbulc/go-turso-kit/migrations"
    "github.com/fightbulc/go-turso-kit/pkg/migrations"
)

// Run migrations
db, _ := sql.Open("turso", "file:./app.db")
err := migrations.Run(context.Background(), db)
```

## Key Points

- ✅ Migrations run in timestamp order
- ✅ Safe to run multiple times (idempotent)
- ✅ Always implement both Up and Down
- ✅ Test rollback before deploying
- ✅ Keep migrations small and focused

## Need Help?

See `README.md` for complete documentation.
