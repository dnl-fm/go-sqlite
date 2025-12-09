# Migration System

A simple, robust migration system for SQLite databases using Go's database/sql.

## Features

- **Registration Pattern**: Migrations register themselves via `init()` functions
- **Version Tracking**: Migrations tracked in `_migrations` table with execution time and duration
- **Atomic Operations**: Each migration runs independently (Up/Down handle their own transactions if needed)
- **Idempotent**: Running migrations multiple times is safe
- **CLI Tool**: Command-line interface for managing migrations
- **Rollback Support**: Roll back to specific versions

## Quick Start

### 1. Create a Migration

Use the CLI tool to generate a migration template:

```bash
export DATABASE_URL="file:./app.db"
migrate create create_users_table
```

This creates a file like `migrations/20251107123456_create_users_table.go`:

```go
package migrations

import (
	"context"
	"database/sql"
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func init() {
	migrations.Register(migrations.Migration{
		Version:     "20251107123456",
		Description: "create_users_table",
		Up:          up20251107123456,
		Down:        down20251107123456,
	})
}

func up20251107123456(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func down20251107123456(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE users`)
	return err
}
```

### 2. Import Migrations in Your Application

In your `main.go`:

```go
import (
	_ "github.com/fightbulc/go-turso-kit/migrations" // Import to register migrations
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)
```

### 3. Run Migrations

#### Using the CLI Tool

```bash
# Run all pending migrations
migrate up

# Check migration status
migrate status

# Rollback last migration
migrate down

# Rollback to specific version
migrate down 20251107123456
```

#### Programmatically

```go
import (
	"context"
	"database/sql"
	_ "github.com/fightbulc/go-turso-kit/migrations"
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func main() {
	db, err := sql.Open("turso", "file:./app.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	// Run all pending migrations
	if err := migrations.Run(ctx, db); err != nil {
		panic(err)
	}
}
```

## API Reference

### Types

```go
type Migration struct {
	Version     string        // Timestamp-based version (e.g., "20251107123456")
	Description string        // Human-readable description
	Up          MigrationFunc // Function to apply migration
	Down        MigrationFunc // Function to rollback migration
}

type MigrationFunc func(context.Context, *sql.DB) error

type MigrationStatus struct {
	Version     string
	Description string
	ExecutedAt  *time.Time // nil if not executed
	DurationMs  int64      // Execution time in milliseconds
}
```

### Functions

```go
// Register a migration (call from init())
func Register(m Migration)

// Run all pending migrations
func Run(ctx context.Context, db *sql.DB) error

// Rollback to a specific version (empty string = last migration)
func Rollback(ctx context.Context, db *sql.DB, version string) error

// Get status of all migrations
func Status(ctx context.Context, db *sql.DB) ([]MigrationStatus, error)

// Get the latest executed migration version
func Latest(ctx context.Context, db *sql.DB) (string, error)
```

## CLI Commands

### Setup

```bash
# Set database URL
export DATABASE_URL="file:./app.db"

# Or for Turso remote database
export DATABASE_URL="https://[db-name]-[org].turso.io"
```

### Commands

```bash
# Create a new migration
migrate create <name>

# Run all pending migrations
migrate up

# Rollback last migration
migrate down

# Rollback to specific version
migrate down <version>

# Show migration status
migrate status
```

### Example Output

```
$ migrate status
Migration Status:

Version              Description                    Status     Executed At          Duration
─────────────────────────────────────────────────────────────────────────────────────────────
20251107000001       create_users                   ✓ done     2025-11-07 12:34:56  12ms
20251107000002       add_posts_table                ✓ done     2025-11-07 12:35:10  8ms
20251107000003       add_user_indexes               pending    -                    -

Total: 3 migrations (2 executed, 1 pending)
```

## Migration Best Practices

### 1. Version Naming

- Use timestamp format: `YYYYMMDDHHMMSS`
- Generated automatically by `migrate create`
- Ensures chronological ordering

### 2. Writing Migrations

**DO:**
- Keep migrations small and focused
- Test both Up and Down functions
- Use explicit SQL (avoid ORM abstractions)
- Handle errors properly
- Add indexes in separate migrations

**DON'T:**
- Mix schema changes with data migrations
- Use database-specific features without fallbacks
- Forget to implement Down functions
- Delete old migrations after they're deployed

### 3. Transaction Handling

Each migration's Up/Down function can manage its own transactions if needed:

```go
func up20251107000001(ctx context.Context, db *sql.DB) error {
	// For DDL that doesn't need transactions
	_, err := db.ExecContext(ctx, `CREATE TABLE users (...)`)
	return err
}

func up20251107000002(ctx context.Context, db *sql.DB) error {
	// For DML that needs atomicity
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `INSERT INTO users ...`); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE settings ...`); err != nil {
		return err
	}

	return tx.Commit()
}
```

### 4. Rollback Safety

- Always test rollback before deploying
- Some migrations can't be rolled back safely (e.g., dropping columns with data)
- Document non-reversible migrations
- Consider data backup strategies

## Testing

```go
import (
	"testing"
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func TestMigrations(t *testing.T) {
	// Reset registry for clean test
	migrations.Reset()
	defer migrations.Reset()

	// Register test migration
	migrations.Register(migrations.Migration{
		Version:     "20251107000001",
		Description: "test_migration",
		Up: func(ctx context.Context, db *sql.DB) error {
			// Test implementation
			return nil
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			return nil
		},
	})

	// Test migration logic
	// ...
}
```

## Migration Tracking

The system creates a `_migrations` table to track execution:

```sql
CREATE TABLE _migrations (
	version TEXT PRIMARY KEY,
	description TEXT NOT NULL,
	executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	duration_ms INTEGER NOT NULL
);
```

## Error Handling

- Failed migrations don't get recorded in `_migrations`
- Migration execution stops on first error
- Rollback failures leave database in inconsistent state (manual intervention required)
- Always backup before running migrations in production

## Examples

See the `examples/migrations/` directory for complete examples:

- Basic migration usage
- Programmatic migration execution
- Status checking
- Rollback handling

## Integration with Database Package

The migration system uses `database/sql` directly, not the `database.Database` wrapper. This provides maximum flexibility:

```go
import (
	"github.com/fightbulc/go-turso-kit/pkg/database"
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func main() {
	// Open database with wrapper
	db, err := database.Open(ctx, "file:./app.db")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// Run migrations using underlying sql.DB
	if err := migrations.Run(ctx, db.DB()); err != nil {
		panic(err)
	}

	// Now use database wrapper for application queries
	// ...
}
```

## FAQ

### Why not use a migration tool like golang-migrate?

This system is designed to be embedded in your application:
- No external dependencies
- Type-safe migration functions
- Integrated with your codebase
- Simple registration pattern

### Can I use this with other databases?

The system is designed for SQLite but uses standard `database/sql`. It should work with other databases with minimal changes:
- Update table creation syntax if needed
- Test transaction behavior
- Adjust timestamp parsing if needed

### How do I handle data migrations?

Create separate migrations for data changes:

```go
func up20251107000010(ctx context.Context, db *sql.DB) error {
	// Migrate data
	_, err := db.ExecContext(ctx, `
		UPDATE users
		SET email_verified = 0
		WHERE email_verified IS NULL
	`)
	return err
}
```

### Can I run migrations in parallel?

No. Migrations run sequentially in version order to ensure consistency.

### What happens if a migration panics?

The migration system will recover and return an error. The migration will not be recorded as executed.
