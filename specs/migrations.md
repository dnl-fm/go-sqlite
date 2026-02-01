---
tags: [migrations, schema, database, cli]
status: documented
owner: pkg/migrations/, cmd/migrate/
extracted_from: pkg/migrations/, cmd/migrate/
---

# Migrations Specification

**Type:** Extraction
**Extracted From:** `pkg/migrations/`, `cmd/migrate/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose

Database migration system for SQLite/Turso providing version-tracked schema changes with a CLI and programmatic API.

### 1.2 Current Capabilities

- Go-based migrations with `init()` registration pattern
- Timestamp-versioned migrations (YYYYMMDDHHMMSS)
- Up/Down migration functions
- `_migrations` table for execution tracking with timing
- CLI for create, up, down, status commands
- Programmatic execution via `Run`, `Rollback`, `Status`, `Latest`
- Idempotent execution (safe to run multiple times)

### 1.3 Boundaries

- SQLite/Turso only (uses turso driver)
- Sequential execution (no parallel migrations)
- No automatic dependency resolution between migrations
- Each migration handles its own transactions (not wrapped by system)

---

## 2. Architecture

### 2.1 Component Structure

```
pkg/migrations/
├── migrations.go   # Registration, types, _migrations table
└── runner.go       # Run, Rollback, Status, Latest

cmd/migrate/
├── main.go         # CLI entry point, usage
└── commands.go     # up, down, status, create implementations

migrations/         # Project migration files
└── YYYYMMDDHHMMSS_name.go
```

### 2.2 Component Diagram

```
┌──────────────────┐
│   cmd/migrate    │
│   (CLI tool)     │
└────────┬─────────┘
         │ calls
         ▼
┌──────────────────┐    ┌───────────────────┐
│  pkg/migrations  │◀───│ migrations/*.go   │
│  - Register()    │    │ (init() calls)    │
│  - Run()         │    └───────────────────┘
│  - Rollback()    │
│  - Status()      │
└────────┬─────────┘
         │ reads/writes
         ▼
┌──────────────────┐
│  _migrations     │
│  (tracking table)│
└──────────────────┘
```

---

## 3. Core Types

### 3.1 Migration

```go
// Source: pkg/migrations/migrations.go:9-15
type MigrationFunc func(context.Context, *sql.DB) error

type Migration struct {
	Version     string        // Timestamp-based: "20251107123456"
	Description string        // Human-readable name
	Up          MigrationFunc // Apply migration
	Down        MigrationFunc // Rollback migration (optional but recommended)
}
```

### 3.2 MigrationStatus

```go
// Source: pkg/migrations/migrations.go:18-23
type MigrationStatus struct {
	Version     string
	Description string
	ExecutedAt  *time.Time // nil if pending
	DurationMs  int64      // Execution time
}
```

### 3.3 Registry

```go
// Source: pkg/migrations/migrations.go:26-28
var (
	registry   = make(map[string]Migration)
	registryMu sync.RWMutex
)
```

Global registry populated by `init()` functions. Thread-safe via mutex.

### 3.4 Tracking Table Schema

```sql
-- Source: pkg/migrations/migrations.go:64-72
CREATE TABLE IF NOT EXISTS _migrations (
	version TEXT PRIMARY KEY,
	description TEXT NOT NULL,
	executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
	duration_ms INTEGER NOT NULL
)
```

---

## 4. Data Flow

### 4.1 Registration Flow

```
1. Import migrations package: _ "project/migrations"
2. Go init() executes for each migration file
3. init() calls migrations.Register(Migration{...})
4. Register validates and adds to global registry
   - Panics on: duplicate version, empty version, nil Up
```

### 4.2 Run Flow (Up)

```go
// Source: pkg/migrations/runner.go:10-39
func Run(ctx context.Context, db *sql.DB) error
```

1. Ensure `_migrations` table exists
2. Query executed migrations from database
3. Get registered migrations (sorted by version)
4. Find pending (registered but not executed)
5. For each pending migration:
   - Execute `Up(ctx, db)`
   - Record in `_migrations` with duration
6. If any fails, stop immediately (partial state)

### 4.3 Rollback Flow

```go
// Source: pkg/migrations/runner.go:72-123
func Rollback(ctx context.Context, db *sql.DB, version string) error
```

1. Get executed migrations
2. If version empty, target = last executed
3. If version specified, validate it was executed
4. Build rollback list (all versions >= target, reverse order)
5. For each:
   - Verify `Down` function exists (error if nil)
   - Execute `Down(ctx, db)`
   - Remove from `_migrations`

### 4.4 Status Flow

```go
// Source: pkg/migrations/runner.go:157-181
func Status(ctx context.Context, db *sql.DB) ([]MigrationStatus, error)
```

Returns all registered migrations with execution status. Merges registry with `_migrations` table data.

### 4.5 Latest Flow

```go
// Source: pkg/migrations/runner.go:184-203
func Latest(ctx context.Context, db *sql.DB) (string, error)
```

Returns most recent executed migration version, or empty string if none.

### 4.6 Current Error Handling

| Error | Behavior |
|-------|----------|
| Duplicate version in Register | panic |
| Empty version | panic |
| Nil Up function | panic |
| Migration Up fails | Error returned, not recorded |
| Migration Down fails | Error returned, record not removed |
| Missing Down function on rollback | Error returned |
| No migrations to rollback | Error returned |

### 4.7 Wiring Map

| From | To | Trigger |
|------|-----|---------|
| migrations/*.go init() | Register() | Package import |
| cmd/migrate up | Run() | CLI command |
| cmd/migrate down | Rollback() | CLI command |
| cmd/migrate status | Status() | CLI command |
| runMigration() | recordMigration() | After successful Up |
| rollbackMigration() | removeMigration() | After successful Down |

---

## 5. CLI

### 5.1 Commands

```bash
# Source: cmd/migrate/main.go:43-53
migrate up              # Run all pending migrations
migrate down [version]  # Rollback to version (or last if empty)
migrate status          # Show migration status table
migrate create <name>   # Generate new migration file
```

### 5.2 Environment

```bash
DATABASE_URL    # Required: "file:./app.db" or Turso URL
```

### 5.3 Create Command

```go
// Source: cmd/migrate/commands.go:145-206
```

1. Read module name from `go.mod`
2. Generate version timestamp
3. Require `migrations/` directory exists
4. Write template to `migrations/YYYYMMDDHHMMSS_name.go`

### 5.4 Status Output Format

```
Migration Status:

Version              Description                    Status     Executed At          Duration
─────────────────────────────────────────────────────────────────────────────────────────────
20251107000001       create_users                   ✓ done     2025-11-07 12:34:56  12ms
20251107000002       add_posts_table                pending    -                    -

Total: 2 migrations (1 executed, 1 pending)
```

---

## 6. Migration File Pattern

### 6.1 Template

```go
// Source: cmd/migrate/commands.go:217-255 (migrationTemplate)
package migrations

import (
	"context"
	"database/sql"
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func init() {
	migrations.Register(migrations.Migration{
		Version:     "{{.Version}}",
		Description: "{{.Description}}",
		Up:          up{{.Version}},
		Down:        down{{.Version}},
	})
}

func up{{.Version}}(ctx context.Context, db *sql.DB) error {
	// TODO: Implement
	return nil
}

func down{{.Version}}(ctx context.Context, db *sql.DB) error {
	// TODO: Implement
	return nil
}
```

### 6.2 Example Migration

```go
// Source: migrations/20251107005645_example_users_table.go
func up20251107005645(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

func down20251107005645(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE users`)
	return err
}
```

---

## 7. Integration

### 7.1 With database.Database

```go
// Use underlying sql.DB for migrations
db, _ := database.Open(ctx, "file:./app.db")
migrations.Run(ctx, db.DB())
```

### 7.2 Import Pattern

```go
// In main.go or app startup
import (
	_ "yourproject/migrations" // Register all migrations
	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)
```

---

## 8. Testing

### 8.1 Test Helpers

```go
// Source: pkg/migrations/migrations.go:155-159
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Migration)
}
```

Clears registry between tests.

### 8.2 Test Pattern

```go
// Source: pkg/migrations/migrations_test.go
func TestRun(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t) // :memory: database
	defer db.Close()

	Register(Migration{...})
	err := Run(ctx, db)
	// assertions
}
```

### 8.3 Test Coverage

| Test | Coverage |
|------|----------|
| TestRegister | Valid, duplicate, empty version, nil Up |
| TestRun | Pending execution, idempotency, order |
| TestRollback | Single, to-version, table drop |
| TestStatus | Pending vs executed display |
| TestLatest | Empty, after migrations |
| TestMigrationError | Failed Up not recorded |
| TestRollbackWithoutDownFunction | Error on nil Down |

---

## 9. Validation

### 9.1 Task Types

- [x] Code changes (extraction only)
- [x] CLI tool

### 9.2 Verification Commands

```bash
cd pkg/migrations && go test -v
```

---

## 10. Gaps and Issues

### 10.1 Missing Error Handling

- [ ] Rollback failure leaves database in inconsistent state
- [ ] No atomic transaction wrapping all pending migrations

### 10.2 Missing Features

- [ ] Dry-run mode
- [ ] Migration locking (concurrent protection)
- [ ] Migration plan preview before execution

### 10.3 Technical Debt

- CLI hardcodes turso driver import
- No confirmation prompt for destructive operations
- `migrate create` requires pre-existing `migrations/` directory

### 10.4 Improvement Opportunities

- Add `--dry-run` flag
- Add `migrate verify` to check if code migrations match executed
- Add migration locking table for concurrent environments
