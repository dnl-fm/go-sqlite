# Go-Turso-Kit: Data Operations Course

A hands-on, step-by-step guide to working with SQLite data using go-turso-kit.

---

## Table of Contents

1. [Setup & Basics](#module-1-setup--basics)
2. [Defining Entities](#module-2-defining-entities)
3. [Building Queries](#module-3-building-queries)
4. [Repository CRUD Operations](#module-4-repository-crud-operations)
5. [Transactions](#module-5-transactions)
6. [Migrations](#module-6-migrations)
7. [ID Generation](#module-7-id-generation)
8. [Time & Timezone Handling](#module-8-time--timezone-handling)
9. [Advanced Patterns](#module-9-advanced-patterns)

---

## Module 1: Setup & Basics

### 1.1 Installation

```bash
go get github.com/fightbulc/go-turso-kit
```

### 1.2 Database Connection

```go
package main

import (
    "database/sql"
    _ "github.com/tursodatabase/turso-go"  // SQLite/Turso driver
)

func main() {
    // In-memory database (for testing)
    db, err := sql.Open("turso", ":memory:")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    // File-based database
    // db, err := sql.Open("turso", "file:myapp.db")

    // Remote Turso database
    // db, err := sql.Open("turso", "libsql://your-db.turso.io?authToken=your-token")
}
```

### 1.3 Creating a Test Table

```go
_, err = db.Exec(`
    CREATE TABLE users (
        id TEXT PRIMARY KEY,
        email TEXT UNIQUE NOT NULL,
        name TEXT NOT NULL,
        active INTEGER DEFAULT 1,
        created_at DATETIME DEFAULT CURRENT_TIMESTAMP
    )
`)
```

---

## Module 2: Defining Entities

### 2.1 Basic Entity Structure

Use `db` struct tags to map columns to fields:

```go
type User struct {
    ID        string `db:"id"`
    Email     string `db:"email"`
    Name      string `db:"name"`
    Active    bool   `db:"active"`
    CreatedAt string `db:"created_at"`
}
```

### 2.2 Entity Design Rules

| Rule | Example |
|------|---------|
| Use `db` tag for column mapping | `db:"column_name"` |
| Tag `db:"-"` to skip fields | `IgnoreMe string \`db:"-"\`` |
| Unexported fields are skipped | `internalCache string` |
| Tag must match column name exactly | `db:"created_at"` not `db:"CreatedAt"` |

### 2.3 Common Field Types

```go
type Product struct {
    ID          string  `db:"id"`          // TEXT PRIMARY KEY
    Name        string  `db:"name"`        // TEXT
    Price       float64 `db:"price"`       // REAL
    Quantity    int     `db:"quantity"`    // INTEGER
    Available   bool    `db:"available"`   // INTEGER (0/1)
    Description *string `db:"description"` // TEXT NULL (use pointer for nullable)
}
```

---

## Module 3: Building Queries

### 3.1 The Query Package

The `query` package converts named parameters (`:name`) to positional placeholders (`?`).

```go
import "github.com/fightbulc/go-turso-kit/pkg/query"
```

### 3.2 Basic Query Building

```go
// Query with parameters
q, err := query.Build(
    "SELECT * FROM users WHERE email = :email",
    map[string]any{"email": "alice@example.com"},
)
if err != nil {
    // Handle error
}

// Execute
rows, err := db.Query(q.SQL(), q.Args()...)
```

### 3.3 Query Without Parameters

```go
// Simple query (no placeholders)
q, err := query.New("SELECT * FROM users")

// This will ERROR if you accidentally include placeholders:
q, err := query.New("SELECT * FROM users WHERE id = :id")  // ERROR!
```

### 3.4 Multiple Parameters

```go
q, err := query.Build(
    "SELECT * FROM users WHERE active = :active AND name LIKE :pattern",
    map[string]any{
        "active":  true,
        "pattern": "%Alice%",
    },
)
```

### 3.5 INSERT Queries

```go
q, err := query.Build(
    `INSERT INTO users (id, email, name, active) 
     VALUES (:id, :email, :name, :active)`,
    map[string]any{
        "id":     "user_123",
        "email":  "alice@example.com",
        "name":   "Alice",
        "active": true,
    },
)
```

### 3.6 UPDATE Queries

```go
q, err := query.Build(
    "UPDATE users SET name = :name, active = :active WHERE id = :id",
    map[string]any{
        "id":     "user_123",
        "name":   "Alice Smith",
        "active": false,
    },
)
```

### 3.7 Query Validation

The `query.Build` function validates parameters:

```go
// ERROR: Missing parameter
q, err := query.Build(
    "SELECT * FROM users WHERE id = :id AND active = :active",
    map[string]any{"id": "123"},  // Missing "active"!
)
// err: "query: missing required parameter: active"

// ERROR: Extra parameter (typo protection)
q, err := query.Build(
    "SELECT * FROM users WHERE id = :id",
    map[string]any{"id": "123", "email": "test@test.com"},  // "email" unused!
)
// err: "query: unused parameter provided: email"
```

---

## Module 4: Repository CRUD Operations

### 4.1 Creating a Repository

```go
import "github.com/fightbulc/go-turso-kit/pkg/repository"

// Generic repository: Repository[EntityType, IDType]
repo := repository.New[User, string](db, "users")
```

### 4.2 FindByID - Get Single Record

```go
ctx := context.Background()

user, err := repo.FindByID(ctx, "user_123")
if errors.Is(err, repository.ErrNotFound) {
    fmt.Println("User not found")
    return
}
if err != nil {
    panic(err)
}

fmt.Printf("Found: %s (%s)\n", user.Name, user.Email)
```

### 4.3 FindAll - Get All Records

```go
users, err := repo.FindAll(ctx)
if err != nil {
    panic(err)
}

for _, u := range users {
    fmt.Printf("- %s: %s\n", u.ID, u.Name)
}
```

### 4.4 FindByQuery - Custom Queries

```go
// Find active users with specific domain
q, _ := query.Build(
    "SELECT * FROM users WHERE active = :active AND email LIKE :domain",
    map[string]any{"active": true, "domain": "%@example.com"},
)

users, err := repo.FindByQuery(ctx, q)
```

### 4.5 FindOneByQuery - Single Result (Nullable)

```go
q, _ := query.Build(
    "SELECT * FROM users WHERE email = :email LIMIT 1",
    map[string]any{"email": "alice@example.com"},
)

user, err := repo.FindOneByQuery(ctx, q)  // Returns *User, not User
if err != nil {
    panic(err)
}

if user == nil {
    fmt.Println("No user found")
} else {
    fmt.Printf("Found: %s\n", user.Name)
}
```

### 4.6 Count & Exists

```go
// Count all records
count, err := repo.Count(ctx)
fmt.Printf("Total users: %d\n", count)

// Check if ID exists
exists, err := repo.Exists(ctx, "user_123")
if exists {
    fmt.Println("User exists")
}
```

### 4.7 Insert

```go
q, _ := query.Build(
    `INSERT INTO users (id, email, name, active) 
     VALUES (:id, :email, :name, :active)`,
    map[string]any{
        "id":     "user_456",
        "email":  "bob@example.com",
        "name":   "Bob",
        "active": true,
    },
)

result, err := repo.Insert(ctx, q)
if err != nil {
    panic(err)
}

rowsAffected, _ := result.RowsAffected()
fmt.Printf("Inserted %d row(s)\n", rowsAffected)
```

### 4.8 Update

```go
q, _ := query.Build(
    "UPDATE users SET name = :name WHERE id = :id",
    map[string]any{
        "id":   "user_456",
        "name": "Bobby",
    },
)

result, err := repo.Update(ctx, q)
```

### 4.9 Delete

```go
// Delete by query
q, _ := query.Build(
    "DELETE FROM users WHERE active = :active",
    map[string]any{"active": false},
)
result, err := repo.Delete(ctx, q)

// Delete by ID (returns error if not found)
err = repo.DeleteByID(ctx, "user_456")
if errors.Is(err, repository.ErrNotFound) {
    fmt.Println("User didn't exist")
}
```

---

## Module 5: Transactions

### 5.1 Basic Transaction

```go
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, string]) error {
    // Insert first user
    q1, _ := query.Build(
        `INSERT INTO users (id, email, name) VALUES (:id, :email, :name)`,
        map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
    )
    if _, err := tx.Insert(ctx, q1); err != nil {
        return err  // Triggers ROLLBACK
    }

    // Insert second user
    q2, _ := query.Build(
        `INSERT INTO users (id, email, name) VALUES (:id, :email, :name)`,
        map[string]any{"id": "2", "email": "bob@test.com", "name": "Bob"},
    )
    if _, err := tx.Insert(ctx, q2); err != nil {
        return err  // Triggers ROLLBACK
    }

    return nil  // Triggers COMMIT
})

if err != nil {
    fmt.Printf("Transaction failed: %v\n", err)
}
```

### 5.2 Transaction Methods

`TxRepository` provides the same methods as `Repository`:

| Method | Description |
|--------|-------------|
| `tx.FindByID()` | Get by ID |
| `tx.FindAll()` | Get all records |
| `tx.FindByQuery()` | Custom query |
| `tx.FindOneByQuery()` | Single result |
| `tx.Count()` | Count records |
| `tx.Exists()` | Check existence |
| `tx.Insert()` | Insert record |
| `tx.Update()` | Update record |
| `tx.Delete()` | Delete by query |
| `tx.DeleteByID()` | Delete by ID |

### 5.3 Conditional Rollback

```go
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, string]) error {
    // Check current count
    count, _ := tx.Count(ctx)
    if count >= 100 {
        return errors.New("user limit reached")  // ROLLBACK
    }

    // Proceed with insert...
    q, _ := query.Build(...)
    _, err := tx.Insert(ctx, q)
    return err
})
```

### 5.4 Raw SQL in Transaction

```go
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, string]) error {
    // Access underlying *sql.Tx for raw operations
    rawTx := tx.Tx()
    
    _, err := rawTx.ExecContext(ctx, "PRAGMA foreign_keys = ON")
    if err != nil {
        return err
    }

    // Continue with repository operations...
    return nil
})
```

---

## Module 6: Migrations

### 6.1 Creating a Migration

Create a file in `migrations/` folder:

```go
// migrations/20251201000001_create_users_table.go
package migrations

import (
    "context"
    "database/sql"
    "github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func init() {
    migrations.Register(migrations.Migration{
        Version:     "20251201000001",
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
}
```

### 6.2 Migration Naming Convention

```
YYYYMMDDHHMMSS_description.go
```

Examples:
- `20251201000001_create_users_table.go`
- `20251201000002_add_users_active_column.go`
- `20251201000003_create_posts_table.go`

### 6.3 Running Migrations

```go
import (
    "github.com/fightbulc/go-turso-kit/pkg/migrations"
    _ "yourapp/migrations"  // Import to register migrations
)

// Run all pending migrations
err := migrations.Run(ctx, db)
if err != nil {
    panic(err)
}
```

### 6.4 Migration Status

```go
statuses, err := migrations.Status(ctx, db)
for _, s := range statuses {
    status := "PENDING"
    if s.ExecutedAt != nil {
        status = fmt.Sprintf("APPLIED (%s)", s.ExecutedAt.Format("2006-01-02 15:04"))
    }
    fmt.Printf("[%s] %s - %s\n", s.Version, s.Description, status)
}
```

Output:
```
[20251201000001] create_users_table - APPLIED (2025-12-01 10:30)
[20251201000002] add_users_active_column - PENDING
```

### 6.5 Rolling Back

```go
// Rollback last migration
err := migrations.Rollback(ctx, db, "")

// Rollback specific version
err := migrations.Rollback(ctx, db, "20251201000002")
```

### 6.6 Multi-Statement Migrations

```go
Up: func(ctx context.Context, db *sql.DB) error {
    statements := []string{
        `CREATE TABLE posts (
            id TEXT PRIMARY KEY,
            user_id TEXT NOT NULL,
            title TEXT NOT NULL,
            FOREIGN KEY (user_id) REFERENCES users(id)
        )`,
        `CREATE INDEX idx_posts_user_id ON posts(user_id)`,
    }

    for _, stmt := range statements {
        if _, err := db.ExecContext(ctx, stmt); err != nil {
            return err
        }
    }
    return nil
},
```

---

## Module 7: ID Generation

### 7.1 ULID - Time-Sortable IDs

ULIDs encode timestamp + randomness in 26 characters.

```go
import "github.com/fightbulc/go-turso-kit/pkg/id/ulid"

// Generate new ULID
id := ulid.New("")
fmt.Println(id.String())  // "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// With prefix
id := ulid.New("user_")
fmt.Println(id.String())  // "user_01ARZ3NDEKTSV4RRFFQ69G5FAV"

// Extract creation time
createdAt := id.Time()
fmt.Println(createdAt)    // 2025-12-09 14:30:00 +0000 UTC
```

### 7.2 ULID Properties

| Property | Value |
|----------|-------|
| Length | 26 characters (+ prefix) |
| Encoding | Crockford Base32 |
| Timestamp | 48-bit (milliseconds) |
| Randomness | 80-bit |
| Sortable | Yes (lexicographically) |
| Valid until | Year 10889 |

### 7.3 Parsing ULIDs

```go
// Parse existing ULID
id, err := ulid.Parse("user_01ARZ3NDEKTSV4RRFFQ69G5FAV")
if err != nil {
    panic(err)
}

fmt.Println(id.Prefix())  // "user_"
fmt.Println(id.Time())    // Creation timestamp
```

### 7.4 NanoID - Compact IDs

NanoIDs are shorter, URL-safe random identifiers.

```go
import "github.com/fightbulc/go-turso-kit/pkg/id/nanoid"

// Default length (21 chars)
id := nanoid.New()
fmt.Println(id.String())  // "V1StGXR8_Z5jdHi6B-myT"

// Custom length
id := nanoid.NewWithLength(10)
fmt.Println(id.String())  // "x8J2k_9pLm"
```

### 7.5 NanoID Properties

| Property | Value |
|----------|-------|
| Default Length | 21 characters |
| Min/Max Length | 6-255 characters |
| Alphabet | `_-0-9a-zA-Z` (64 chars) |
| Entropy | ~6 bits per character |
| Sortable | No (random) |

### 7.6 When to Use Which?

| Use Case | Recommendation |
|----------|----------------|
| Primary keys (ordered) | ULID |
| API tokens | NanoID |
| Short URLs | NanoID (custom length) |
| Audit trails | ULID (timestamp extraction) |
| External references | ULID with prefix (`order_`, `inv_`) |

---

## Module 8: Time & Timezone Handling

### 8.1 The Zeit Package

`Zeit` provides timezone-aware datetime operations.

```go
import "github.com/fightbulc/go-turso-kit/pkg/zeit"
```

### 8.2 Creating Zeit Instances

```go
// Current time in timezone
tokyo, _ := time.LoadLocation("Asia/Tokyo")
z := zeit.Now(tokyo)

// From existing time
t := time.Now()
z := zeit.New(t, tokyo)

// From ISO 8601 string
z, err := zeit.FromUser("2025-12-09T14:30:00+09:00", tokyo)

// From database (Unix timestamp)
z := zeit.FromDatabase(1733748600, tokyo)
```

### 8.3 Database Storage

```go
// Store as Unix timestamp (int64)
timestamp := z.ToDatabase()  // 1733748600

// Store in database
q, _ := query.Build(
    "INSERT INTO events (id, name, scheduled_at) VALUES (:id, :name, :scheduled)",
    map[string]any{
        "id":        "evt_123",
        "name":      "Meeting",
        "scheduled": z.ToDatabase(),
    },
)

// Retrieve from database
// (assuming scheduled_at column returns int64)
z := zeit.FromDatabase(row.ScheduledAt, userTimezone)
```

### 8.4 Date Arithmetic

```go
// Add duration
later := z.Add(2 * time.Hour)

// Add days
tomorrow := z.AddDays(1)
nextWeek := z.AddDays(7)
lastMonth := z.AddDays(-30)

// Add business days (skips weekends)
nextBusinessDay := z.AddBusinessDays(1)
fiveBusinessDays := z.AddBusinessDays(5)
```

### 8.5 Formatting & Comparison

```go
// Format for display
display := z.Format("2006-01-02 15:04")  // "2025-12-09 14:30"

// ISO 8601 for API responses
iso := z.ToUser()  // "2025-12-09T14:30:00+09:00"

// Comparisons
if deadline.Before(z) {
    fmt.Println("Deadline passed")
}

if z.Equal(other) {
    fmt.Println("Same moment in time")
}
```

### 8.6 Billing Cycles

Generate billing periods:

```go
// Monthly billing cycles starting from subscription date
startDate := zeit.Now(userTimezone)
cycles := startDate.Cycles(12, zeit.Monthly)

for i, period := range cycles {
    fmt.Printf("Period %d: %s to %s\n",
        i+1,
        period.StartsAt.Format("2006-01-02"),
        period.EndsAt.Format("2006-01-02"),
    )
}
```

Output:
```
Period 1: 2025-12-09 to 2026-01-08
Period 2: 2026-01-09 to 2026-02-08
...
```

---

## Module 9: Advanced Patterns

### 9.1 Multiple Repositories

```go
type User struct {
    ID    string `db:"id"`
    Email string `db:"email"`
    Name  string `db:"name"`
}

type Post struct {
    ID        string `db:"id"`
    UserID    string `db:"user_id"`
    Title     string `db:"title"`
    CreatedAt int64  `db:"created_at"`
}

// Create separate repositories
userRepo := repository.New[User, string](db, "users")
postRepo := repository.New[Post, string](db, "posts")
```

### 9.2 Service Layer Pattern

```go
type UserService struct {
    repo *repository.Repository[User, string]
}

func NewUserService(db *sql.DB) *UserService {
    return &UserService{
        repo: repository.New[User, string](db, "users"),
    }
}

func (s *UserService) CreateUser(ctx context.Context, email, name string) (*User, error) {
    id := ulid.New("user_")

    q, err := query.Build(
        `INSERT INTO users (id, email, name) VALUES (:id, :email, :name)`,
        map[string]any{
            "id":    id.String(),
            "email": email,
            "name":  name,
        },
    )
    if err != nil {
        return nil, err
    }

    if _, err := s.repo.Insert(ctx, q); err != nil {
        return nil, err
    }

    return s.repo.FindByID(ctx, id.String())
}

func (s *UserService) GetByEmail(ctx context.Context, email string) (*User, error) {
    q, _ := query.Build(
        "SELECT * FROM users WHERE email = :email LIMIT 1",
        map[string]any{"email": email},
    )
    return s.repo.FindOneByQuery(ctx, q)
}
```

### 9.3 Pagination

```go
func (s *UserService) ListUsers(ctx context.Context, page, pageSize int) ([]User, error) {
    offset := (page - 1) * pageSize

    q, _ := query.Build(
        `SELECT * FROM users ORDER BY created_at DESC LIMIT :limit OFFSET :offset`,
        map[string]any{
            "limit":  pageSize,
            "offset": offset,
        },
    )

    return s.repo.FindByQuery(ctx, q)
}
```

### 9.4 Soft Deletes

```go
type User struct {
    ID        string `db:"id"`
    Email     string `db:"email"`
    Name      string `db:"name"`
    DeletedAt *int64 `db:"deleted_at"`  // NULL = not deleted
}

// Find active users only
func (s *UserService) FindActive(ctx context.Context) ([]User, error) {
    q, _ := query.New("SELECT * FROM users WHERE deleted_at IS NULL")
    return s.repo.FindByQuery(ctx, q)
}

// Soft delete
func (s *UserService) SoftDelete(ctx context.Context, id string) error {
    now := zeit.Now(time.UTC).ToDatabase()
    q, _ := query.Build(
        "UPDATE users SET deleted_at = :deleted WHERE id = :id",
        map[string]any{"id": id, "deleted": now},
    )
    _, err := s.repo.Update(ctx, q)
    return err
}
```

### 9.5 Cross-Transaction Reads

```go
err := repo.WithTx(ctx, func(tx *repository.TxRepository[User, string]) error {
    // Check user exists (within transaction)
    exists, _ := tx.Exists(ctx, "user_123")
    if !exists {
        return errors.New("user not found")
    }

    // Read from another table using raw tx
    var balance float64
    err := tx.Tx().QueryRowContext(ctx,
        "SELECT balance FROM wallets WHERE user_id = ?", "user_123",
    ).Scan(&balance)
    if err != nil {
        return err
    }

    if balance < 10.0 {
        return errors.New("insufficient balance")
    }

    // Proceed with operation...
    return nil
})
```

---

## Quick Reference

### Import Paths

```go
import (
    "github.com/fightbulc/go-turso-kit/pkg/database"
    "github.com/fightbulc/go-turso-kit/pkg/query"
    "github.com/fightbulc/go-turso-kit/pkg/repository"
    "github.com/fightbulc/go-turso-kit/pkg/scan"
    "github.com/fightbulc/go-turso-kit/pkg/migrations"
    "github.com/fightbulc/go-turso-kit/pkg/id/ulid"
    "github.com/fightbulc/go-turso-kit/pkg/id/nanoid"
    "github.com/fightbulc/go-turso-kit/pkg/zeit"
)
```

### Common Errors

| Error | Cause |
|-------|-------|
| `repository.ErrNotFound` | `FindByID`/`DeleteByID` found no matching row |
| `repository.ErrNilDB` | Repository created with nil database |
| `query.ErrMissingParam` | SQL placeholder without matching param |
| `query.ErrExtraParam` | Param provided but not used in SQL |
| `query.ErrEmptySQL` | Empty SQL string |

### Cheat Sheet

```go
// Query building
q, _ := query.Build("SELECT * FROM t WHERE x = :x", map[string]any{"x": 1})
q, _ := query.New("SELECT * FROM t")

// Repository
repo := repository.New[T, ID](db, "table")
entity, _ := repo.FindByID(ctx, id)
entities, _ := repo.FindAll(ctx)
entities, _ := repo.FindByQuery(ctx, q)
entity, _ := repo.FindOneByQuery(ctx, q)  // returns *T
count, _ := repo.Count(ctx)
exists, _ := repo.Exists(ctx, id)
result, _ := repo.Insert(ctx, q)
result, _ := repo.Update(ctx, q)
result, _ := repo.Delete(ctx, q)
err := repo.DeleteByID(ctx, id)

// Transactions
repo.WithTx(ctx, func(tx *repository.TxRepository[T, ID]) error { ... })

// Migrations
migrations.Register(m)
migrations.Run(ctx, db)
migrations.Status(ctx, db)
migrations.Rollback(ctx, db, "")

// IDs
ulid.New("prefix_")
nanoid.New()
nanoid.NewWithLength(10)

// Zeit
z := zeit.Now(loc)
z := zeit.FromDatabase(timestamp, loc)
timestamp := z.ToDatabase()
z.AddDays(1)
z.AddBusinessDays(5)
```

---

*Course version: 1.0 | go-turso-kit*
