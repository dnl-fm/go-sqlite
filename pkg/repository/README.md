# repository

Generic repository pattern for type-safe CRUD operations using Go generics.

## Overview

The `repository` package provides a generic repository pattern that eliminates boilerplate code while maintaining type safety through Go 1.18+ generics.

## Features

- Type-safe CRUD operations (Create, Read, Update, Delete)
- Go generics (no reflection overhead)
- Transaction support with automatic rollback
- Custom query support
- Compile-time type checking

## Installation

```bash
go get github.com/fightbulc/go-turso-kit/pkg/repository
```

## Quick Start

### 1. Define Entity

```go
type User struct {
    ID        string `db:"id"`
    Email     string `db:"email"`
    Username  string `db:"username"`
    CreatedAt string `db:"created_at"`
}

// Implement Entity interface
func (u User) TableName() string {
    return "users"
}

func (u User) PrimaryKey() string {
    return "id"
}
```

### 2. Create Repository

```go
import "github.com/fightbulc/go-turso-kit/pkg/repository"

db := setupDatabase()  // *sql.DB
repo := repository.New[User](db)
```

### 3. Use CRUD Operations

```go
ctx := context.Background()

// Create
user := User{
    ID:       "user_123",
    Email:    "alice@example.com",
    Username: "alice",
}
err := repo.Create(ctx, &user)

// Find by ID
found, err := repo.FindByID(ctx, "user_123")

// Find all
users, err := repo.FindAll(ctx)

// Update
found.Email = "newemail@example.com"
err = repo.Update(ctx, found)

// Delete
err = repo.Delete(ctx, "user_123")
```

## Entity Interface

Entities must implement the `Entity` interface:

```go
type Entity interface {
    TableName() string   // Database table name
    PrimaryKey() string  // Primary key column name
}
```

**Example:**
```go
type Post struct {
    ID      string `db:"id"`
    Title   string `db:"title"`
    Content string `db:"content"`
}

func (p Post) TableName() string  { return "posts" }
func (p Post) PrimaryKey() string { return "id" }
```

## API Reference

### New

Creates a new repository for entity type T.

```go
func New[T Entity](db *sql.DB) *Repository[T]
```

**Example:**
```go
userRepo := repository.New[User](db)
postRepo := repository.New[Post](db)
```

### Create

Inserts a new entity into the database.

```go
func (r *Repository[T]) Create(ctx context.Context, entity *T) error
```

**Example:**
```go
user := &User{
    ID:    "user_123",
    Email: "alice@example.com",
}
err := repo.Create(ctx, user)
```

**Notes:**
- Accepts pointer to entity
- Uses struct tags to map fields to columns
- Returns error if insert fails

### FindByID

Finds an entity by its primary key.

```go
func (r *Repository[T]) FindByID(ctx context.Context, id interface{}) (*T, error)
```

**Example:**
```go
user, err := repo.FindByID(ctx, "user_123")
if err != nil {
    if errors.Is(err, repository.ErrNotFound) {
        // Handle not found
    }
    return err
}

fmt.Printf("Found: %s\n", user.Email)
```

**Returns:**
- `*T` - Pointer to found entity
- `error` - `ErrNotFound` if not found, other errors on query failure

### FindAll

Retrieves all entities from the table.

```go
func (r *Repository[T]) FindAll(ctx context.Context) ([]T, error)
```

**Example:**
```go
users, err := repo.FindAll(ctx)
if err != nil {
    return err
}

for _, user := range users {
    fmt.Printf("%s: %s\n", user.ID, user.Email)
}
```

**Notes:**
- Returns empty slice if no results
- May return many rows (consider pagination)

### Update

Updates an existing entity.

```go
func (r *Repository[T]) Update(ctx context.Context, entity *T) error
```

**Example:**
```go
user, _ := repo.FindByID(ctx, "user_123")
user.Email = "newemail@example.com"
err := repo.Update(ctx, user)
```

**Notes:**
- Updates by primary key
- Updates all fields
- Returns error if entity not found

### Delete

Deletes an entity by primary key.

```go
func (r *Repository[T]) Delete(ctx context.Context, id interface{}) error
```

**Example:**
```go
err := repo.Delete(ctx, "user_123")
if err != nil {
    return err
}
```

**Notes:**
- Silent success if entity doesn't exist
- Returns error on query failure

### Transaction

Executes a function within a transaction, with automatic rollback on error.

```go
func (r *Repository[T]) Transaction(ctx context.Context, fn func(*Repository[T]) error) error
```

**Example:**
```go
err := repo.Transaction(ctx, func(txRepo *repository.Repository[User]) error {
    // Create user
    user := &User{ID: "user_123", Email: "alice@example.com"}
    if err := txRepo.Create(ctx, user); err != nil {
        return err  // Automatic rollback
    }

    // Create another user
    user2 := &User{ID: "user_456", Email: "bob@example.com"}
    if err := txRepo.Create(ctx, user2); err != nil {
        return err  // Automatic rollback
    }

    return nil  // Commit
})
```

**Behavior:**
- Begins transaction
- Calls function with transaction-scoped repository
- Commits if function returns nil
- Rolls back if function returns error

## Error Handling

### Standard Errors

```go
var ErrNotFound = errors.New("entity not found")
```

**Usage:**
```go
user, err := repo.FindByID(ctx, "user_123")
if err != nil {
    if errors.Is(err, repository.ErrNotFound) {
        // Handle not found
        return fmt.Errorf("user not found")
    }
    // Other error
    return err
}
```

## Struct Tags

Use `db` struct tags to map fields to database columns:

```go
type User struct {
    ID        string `db:"id"`           // Maps to "id" column
    Email     string `db:"email"`        // Maps to "email" column
    CreatedAt string `db:"created_at"`   // Maps to "created_at" column
}
```

**Tag format:** `` `db:"column_name"` ``

## Transactions

Repositories support transactions with automatic rollback:

```go
err := userRepo.Transaction(ctx, func(txRepo *repository.Repository[User]) error {
    // All operations use the same transaction
    err := txRepo.Create(ctx, &user1)
    if err != nil {
        return err  // Rollback
    }

    err = txRepo.Update(ctx, &user2)
    if err != nil {
        return err  // Rollback
    }

    // Commit on nil return
    return nil
})

if err != nil {
    // Transaction was rolled back
}
```

**Important:**
- Use transaction-scoped repository (`txRepo`)
- Don't use original repository inside transaction
- Return error to rollback, nil to commit

## Best Practices

### 1. Use Pointers for Create/Update

```go
// ✅ Correct
user := &User{ID: "123", Email: "alice@example.com"}
repo.Create(ctx, user)

// ❌ Wrong
user := User{ID: "123", Email: "alice@example.com"}
repo.Create(ctx, &user)  // Less efficient
```

### 2. Check ErrNotFound

```go
user, err := repo.FindByID(ctx, id)
if err != nil {
    if errors.Is(err, repository.ErrNotFound) {
        return nil, fmt.Errorf("user %s not found", id)
    }
    return nil, err
}
```

### 3. Use Transactions for Multi-Entity Operations

```go
// Ensures both succeed or both fail
err := repo.Transaction(ctx, func(txRepo *repository.Repository[User]) error {
    err := txRepo.Create(ctx, &user1)
    err = txRepo.Create(ctx, &user2)
    return err
})
```

### 4. Pagination for FindAll

```go
// For large tables, use custom query with LIMIT/OFFSET
// Don't use FindAll() for tables with millions of rows
```

## Custom Queries

For custom queries beyond CRUD, use the underlying database:

```go
db := repo.DB()  // Access underlying *sql.DB

rows, err := db.QueryContext(ctx,
    "SELECT * FROM users WHERE email LIKE ? LIMIT 10",
    "%@example.com",
)
defer rows.Close()

// Manual scanning
for rows.Next() {
    var user User
    rows.Scan(&user.ID, &user.Email, ...)
}
```

Or use the query builder:

```go
import "github.com/fightbulc/go-turso-kit/pkg/query"

q := query.New("users").
    Select("*").
    Where("email LIKE ?", "%@example.com").
    Limit(10)

sql, args := q.Build()
rows, err := db.QueryContext(ctx, sql, args...)
```

## Examples

See [examples/repository/main.go](../../examples/repository/main.go) for complete working examples.

## Type Safety

Repository provides compile-time type safety:

```go
// ✅ Type-safe
userRepo := repository.New[User](db)
user, err := userRepo.FindByID(ctx, "123")  // Returns *User

// ❌ Compile error
user, err := userRepo.FindByID(ctx, 123)  // Wrong type for ID
post := userRepo.Create(ctx, &post)       // Wrong type for entity
```

## Performance

- **Zero reflection overhead** - Generics compile to concrete types
- **Prepared statements** - Used internally for efficiency
- **Minimal allocations** - ~512 bytes per operation

See [PERFORMANCE.md](../../PERFORMANCE.md) for benchmarks.

## See Also

- [database](../database/README.md) - Database connection
- [query](../query/README.md) - Query builder
- [examples/repository](../../examples/repository/) - Complete examples
