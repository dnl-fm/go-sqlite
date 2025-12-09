# Examples

Working examples demonstrating go-turso-kit features.

## Running Examples

```bash
# From project root
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```

---

## Repository Example

**File:** `repository/main.go`

Demonstrates the repository pattern with automatic struct scanning.

### What it shows

- Creating a repository with `db` struct tags (no mapper function needed)
- CRUD operations: Insert, FindByID, FindAll, Update, Delete
- Custom queries with named parameters
- Counting and existence checks
- Transactions with multiple operations

### Key code

```go
// Define entity with db tags
type User struct {
    ID    int    `db:"id"`
    Email string `db:"email"`
    Name  string `db:"name"`
}

// Create repository - no mapper needed
repo := repository.New[User, int](db, "users")

// Find by ID
user, err := repo.FindByID(ctx, 1)

// Custom query with named params
q, _ := query.Build(
    "SELECT * FROM users WHERE email LIKE :pattern",
    map[string]any{"pattern": "%@example.com"},
)
users, err := repo.FindByQuery(ctx, q)

// Transaction
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, int]) error {
    q, _ := query.Build("INSERT INTO users ...", params)
    _, err := tx.Insert(ctx, q)
    return err
})
```

---

## Transactions Example

**File:** `transactions/main.go`

Demonstrates transaction commit and rollback behavior.

### What it shows

- Bank account transfer scenario
- Automatic rollback on error
- Transaction isolation
- Balance validation within transaction

### Key code

```go
func transfer(ctx context.Context, repo *repository.Repository[Account, int], fromID, toID int, amount float64) error {
    return repo.WithTx(ctx, func(tx *repository.TxRepository[Account, int]) error {
        // Check balance
        from, err := tx.FindByID(ctx, fromID)
        if from.Balance < amount {
            return fmt.Errorf("insufficient funds")  // triggers rollback
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

---

## Migrations Example

**File:** `migrations/main.go`

Demonstrates database schema migrations.

### What it shows

- Registering migrations with version and description
- Running pending migrations
- Checking migration status
- Rolling back migrations
- Creating tables, foreign keys, and indexes

### Key code

```go
// Register migration
migrations.Register(migrations.Migration{
    Version:     "20251107000001",
    Description: "create_users_table",
    Up:          func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `
            CREATE TABLE users (
                id TEXT PRIMARY KEY,
                email TEXT UNIQUE NOT NULL,
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

// Run all pending migrations
migrations.Run(ctx, db)

// Check status
statuses, _ := migrations.Status(ctx, db)

// Rollback last migration
migrations.Rollback(ctx, db, "")
```

---

## Timezones Example

**File:** `timezones/main.go`

Demonstrates the Zeit package for timezone-aware datetime handling.

### What it shows

- Current time in multiple timezones
- Database serialization (Unix timestamps)
- Date arithmetic (add days, weeks)
- Business day calculations (skip weekends)
- Billing cycle generation (weekly, monthly, quarterly)
- Duration calculations

### Key code

```go
// Create Zeit in timezone
tokyo, _ := time.LoadLocation("Asia/Tokyo")
z := zeit.Now(tokyo)

// Database storage (UTC Unix timestamp)
timestamp := z.ToDatabase()  // int64
restored := zeit.FromDatabase(timestamp, tokyo)

// Business days (skip weekends)
friday := zeit.New(time.Date(2024, 1, 12, 10, 0, 0, 0, utc), utc)
monday := friday.AddBusinessDays(1)  // skips Sat/Sun

// Billing cycles
startDate := zeit.New(time.Date(2024, 1, 15, 0, 0, 0, 0, utc), utc)
cycles := startDate.Cycles(12, zeit.Monthly)
for _, period := range cycles {
    fmt.Printf("%s to %s\n", 
        period.StartsAt.Format("2006-01-02"),
        period.EndsAt.Format("2006-01-02"))
}

// Duration between dates
duration := zeit.NewDuration(start, end)
fmt.Println(duration.Days())         // calendar days
fmt.Println(duration.BusinessDays()) // weekdays only
```

---

## Quick Reference

| Example | Package | Key Features |
|---------|---------|--------------|
| repository | `pkg/repository`, `pkg/query`, `pkg/scan` | CRUD, struct tags, named params |
| transactions | `pkg/repository` | Commit, rollback, isolation |
| migrations | `pkg/migrations` | Schema versioning, up/down |
| timezones | `pkg/zeit` | Timezone handling, billing cycles |
