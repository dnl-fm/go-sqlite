---
tags: [examples, tutorial, usage]
status: documented
owner: tmp/examples/
extracted_from: tmp/examples/
---

# Examples Specification

**Type:** Extraction
**Extracted From:** `tmp/examples/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose
Working examples demonstrating go-turso-kit package usage. Each example is a standalone main.go that can be run independently.

### 1.2 Current Capabilities
- Repository pattern with struct tag scanning
- Transaction commit/rollback patterns
- Programmatic migration management
- Zeit timezone and billing cycle operations

### 1.3 Boundaries
- Examples use in-memory SQLite only
- No CLI flag parsing
- No configuration files

---

## 2. Architecture

### 2.1 Directory Structure
```
tmp/examples/
├── README.md            # Usage docs with code snippets
├── migrations/main.go   # Migration workflow demo
├── repository/main.go   # CRUD and query operations
├── timezones/main.go    # Zeit datetime utilities
└── transactions/main.go # Transaction rollback demo
```

### 2.2 Package Dependencies

| Example | Uses Packages |
|---------|---------------|
| migrations | `pkg/migrations` |
| repository | `pkg/repository`, `pkg/query` |
| timezones | `pkg/zeit` |
| transactions | `pkg/repository`, `pkg/query` |

---

## 3. Migrations Example

**Source:** `tmp/examples/migrations/main.go`

### 3.1 Purpose
Demonstrates programmatic migration registration and execution (vs CLI tool).

### 3.2 Pattern

```go
// Source: tmp/examples/migrations/main.go:71-95
migrations.Register(migrations.Migration{
    Version:     "20251107000001",
    Description: "create_users_table",
    Up:          up20251107000001,
    Down:        down20251107000001,
})

// Source: tmp/examples/migrations/main.go:98-116
func up20251107000001(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        CREATE TABLE users (
            id TEXT PRIMARY KEY,
            email TEXT UNIQUE NOT NULL,
            username TEXT NOT NULL,
            password_hash TEXT NOT NULL,
            created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
            updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
        )
    `)
    if err != nil {
        return fmt.Errorf("failed to create users table: %w", err)
    }
    return nil
}
```

### 3.3 Operations Demonstrated

| Function | Purpose |
|----------|---------|
| `migrations.Register()` | Add migration to registry |
| `migrations.Run()` | Execute pending migrations |
| `migrations.Status()` | List all migrations with state |
| `migrations.Latest()` | Get current version |
| `migrations.Rollback()` | Undo last migration |

### 3.4 Workflow
1. Register migrations in order
2. Check initial status (all pending)
3. Run all pending migrations
4. Verify tables created
5. Rollback last migration
6. Re-run (idempotent)

---

## 4. Repository Example

**Source:** `tmp/examples/repository/main.go`

### 4.1 Entity Definition

```go
// Source: tmp/examples/repository/main.go:15-19
type User struct {
    ID    int    `db:"id"`
    Name  string `db:"name"`
    Email string `db:"email"`
}
```

### 4.2 Repository Creation

```go
// Source: tmp/examples/repository/main.go:38
userRepo := repository.New[User, int](db, "users")
```

No mapper function needed - uses `db` struct tags for automatic scanning.

### 4.3 Operations Demonstrated

| Operation | Method | Notes |
|-----------|--------|-------|
| Insert | via raw SQL | Initial inserts use `db.ExecContext` |
| Find by ID | `repo.FindByID(ctx, 1)` | Returns single entity |
| Find all | `repo.FindAll(ctx)` | Returns slice |
| Update | `repo.Update(ctx, q)` | Uses query builder |
| Delete | `repo.DeleteByID(ctx, 1)` | By ID |
| Count | `repo.Count(ctx)` | Total count |
| Exists | `repo.Exists(ctx, id)` | Boolean check |
| Custom query | `repo.FindByQuery(ctx, q)` | Named params |

### 4.4 Query Building Pattern

```go
// Source: tmp/examples/repository/main.go:70-74
q, err := query.Build(
    "UPDATE users SET name = :name, email = :email WHERE id = :id",
    map[string]any{"id": 1, "name": "Alicia", "email": "alicia@example.com"},
)
_, err = userRepo.Update(ctx, q)
```

### 4.5 Transaction Pattern

```go
// Source: tmp/examples/repository/main.go:115-137
err = userRepo.WithTx(ctx, func(txRepo *repository.TxRepository[User, int]) error {
    q, err := query.Build(
        "INSERT INTO users (name, email) VALUES (:name, :email)",
        map[string]any{"name": "Charlie", "email": "charlie@example.com"},
    )
    if err != nil {
        return err
    }
    _, err = txRepo.Insert(ctx, q)
    return err
})
```

---

## 5. Transactions Example

**Source:** `tmp/examples/transactions/main.go`

### 5.1 Entity Definition

```go
// Source: tmp/examples/transactions/main.go:14-18
type Account struct {
    ID      int     `db:"id"`
    Name    string  `db:"name"`
    Balance float64 `db:"balance"`
}
```

### 5.2 Transfer Function Pattern

```go
// Source: tmp/examples/transactions/main.go:73-108
func transfer(ctx context.Context, repo *repository.Repository[Account, int], 
              fromID, toID int, amount float64) error {
    return repo.WithTx(ctx, func(tx *repository.TxRepository[Account, int]) error {
        // Get source account
        from, err := tx.FindByID(ctx, fromID)
        if err != nil {
            return fmt.Errorf("source account not found: %w", err)
        }

        // Check sufficient funds
        if from.Balance < amount {
            return fmt.Errorf("insufficient funds: have %.2f, need %.2f", 
                from.Balance, amount)
        }

        // Debit source
        q, _ := query.Build(
            "UPDATE accounts SET balance = balance - :amount WHERE id = :id",
            map[string]any{"id": fromID, "amount": amount},
        )
        tx.Update(ctx, q)

        // Credit destination
        q, _ = query.Build(
            "UPDATE accounts SET balance = balance + :amount WHERE id = :id",
            map[string]any{"id": toID, "amount": amount},
        )
        tx.Update(ctx, q)

        return nil  // commit
    })
}
```

### 5.3 Rollback Behavior
- Returning error from `WithTx` callback triggers automatic rollback
- Successful return (nil) commits the transaction
- Example tests both successful transfer and insufficient funds rollback

---

## 6. Timezones Example

**Source:** `tmp/examples/timezones/main.go`

### 6.1 Operations Demonstrated

| Operation | Code | Notes |
|-----------|------|-------|
| Current time | `zeit.Now(location)` | Timezone-aware now |
| Create from time | `zeit.New(t, loc)` | Wrap time.Time |
| Database storage | `z.ToDatabase()` | Returns int64 Unix |
| Restore from DB | `zeit.FromDatabase(ts, loc)` | Restore with timezone |
| Parse ISO string | `zeit.FromUser(str, loc)` | Parse user input |
| Format for user | `z.ToUser()` | ISO string output |

### 6.2 Date Arithmetic

```go
// Source: tmp/examples/timezones/main.go:56-63
today := zeit.Now(utc)
tomorrow := today.AddDays(1)
nextWeek := today.AddDays(7)
```

### 6.3 Business Day Arithmetic

```go
// Source: tmp/examples/timezones/main.go:68-74
friday := time.Date(2024, 1, 12, 10, 0, 0, 0, utc) // Friday
z := zeit.New(friday, utc)
nextBusDay := z.AddBusinessDays(1)  // Returns Monday, skips Sat/Sun
```

### 6.4 Billing Cycles

```go
// Source: tmp/examples/timezones/main.go:85-96
startDate := zeit.New(time.Date(2024, 1, 15, 0, 0, 0, 0, utc), utc)

// Monthly cycles
cycles := startDate.Cycles(3, zeit.Monthly)
for _, period := range cycles {
    fmt.Printf("%s to %s\n",
        period.StartsAt.Format("2006-01-02"),
        period.EndsAt.Format("2006-01-02"))
}

// Also supports: zeit.Weekly, zeit.Quarterly
```

### 6.5 Duration Calculations

```go
// Source: tmp/examples/timezones/main.go:118-127
start := zeit.New(time.Date(2024, 1, 1, 0, 0, 0, 0, utc), utc)
end := zeit.New(time.Date(2024, 1, 10, 12, 30, 0, 0, utc), utc)

duration := zeit.NewDuration(start, end)
duration.Days()         // Calendar days
duration.BusinessDays() // Mon-Fri only
duration.Hours()        // Total hours
```

---

## 7. Running Examples

```bash
# From project root
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```

All examples:
- Use in-memory SQLite (`:memory:`)
- Import driver: `_ "turso.tech/database/tursogo"`
- Self-contained with table creation
- Print step-by-step output with ✓ checkmarks

---

## 10. Gaps and Issues

### 10.1 Documentation Gaps
- [ ] README snippets slightly simplified vs actual code (acceptable)
- [ ] No error handling examples for edge cases

### 10.2 Missing Examples
- [ ] No ID generation example (nanoid, ulid packages)
- [ ] No scan package standalone example
- [ ] No query package edge cases (missing params, validation)

### 10.3 Improvement Opportunities
- Add id generation example showing NanoID vs ULID use cases
- Add query validation example with error handling
- Add example with real file database (not :memory:)
