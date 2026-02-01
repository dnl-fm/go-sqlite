---
tags: [repository, crud, transactions, generics]
status: documented
owner: pkg/repository/
extracted_from: pkg/repository/
---

# Repository Specification

**Type:** Extraction
**Extracted From:** `pkg/repository/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose

Generic repository pattern for type-safe CRUD operations using Go generics. Provides query-based data access with transaction support.

### 1.2 Current Capabilities

- Generic CRUD operations via `Repository[T any, ID comparable]`
- Transaction support with automatic rollback via `TxRepository`
- Query-based insert/update/delete (not entity-based)
- Integrates with `pkg/query` for named parameters
- Integrates with `pkg/scan` for row scanning

### 1.3 Boundaries

- Does not auto-generate SQL from entities
- Does not implement Entity interface (README claims one but it doesn't exist)
- Insert/Update/Delete require pre-built Query objects
- Table name passed at construction, not derived from type

---

## 2. Architecture

### 2.1 Component Structure

```
pkg/repository/
├── repository.go       # Repository[T, ID] type, CRUD operations
├── transaction.go      # TxRepository[T, ID], WithTx
├── repository_test.go  # Unit tests
├── transaction_test.go # Transaction tests
├── benchmark_test.go   # Performance benchmarks
└── README.md           # Documentation (outdated)
```

### 2.2 Component Diagram

```
┌─────────────────┐     ┌─────────────────┐
│  Repository     │────▶│   TxRepository  │
│  [T, ID]        │     │   [T, ID]       │
└────────┬────────┘     └────────┬────────┘
         │                       │
         ▼                       ▼
┌─────────────────┐     ┌─────────────────┐
│   pkg/query     │     │    *sql.Tx      │
│   Query.Build   │     │                 │
└─────────────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐
│   pkg/scan      │
│   Row/All/One   │
└─────────────────┘
```

### 2.3 Dependencies

| Package | Usage |
|---------|-------|
| `pkg/query` | Named parameter query building |
| `pkg/scan` | Generic row scanning with `db` tags |
| `database/sql` | Standard library database interface |

---

## 3. Core Types

### 3.1 Repository

```go
// Source: pkg/repository/repository.go:35-40
type Repository[T any, ID comparable] struct {
    db        *sql.DB
    tableName string
}
```

**Type Parameters:**
- `T` - Entity type (must have `db` struct tags for scanning)
- `ID` - Primary key type (e.g., `string`, `int`)

**Constructor:**

```go
// Source: pkg/repository/repository.go:45-49
func New[T any, ID comparable](db *sql.DB, tableName string) *Repository[T, ID]
```

### 3.2 TxRepository

```go
// Source: pkg/repository/transaction.go:12-15
type TxRepository[T any, ID comparable] struct {
    tx        *sql.Tx
    tableName string
}
```

Mirrors `Repository` API but operates on `*sql.Tx` instead of `*sql.DB`.

### 3.3 Errors

```go
// Source: pkg/repository/repository.go:14-20
var (
    ErrNotFound       = errors.New("repository: entity not found")
    ErrNilDB          = errors.New("repository: database cannot be nil")
    ErrEmptyTableName = errors.New("repository: table name cannot be empty")
)
```

### 3.4 Entity Example

```go
// Source: pkg/repository/repository_test.go:12-16
type testUser struct {
    ID    string `db:"id"`
    Email string `db:"email"`
    Name  string `db:"name"`
}
```

---

## 4. Data Flow

### 4.1 Repository Methods

| Method | Query | Returns | Notes |
|--------|-------|---------|-------|
| `FindByID(ctx, id)` | `SELECT * FROM {table} WHERE id = :id` | `(T, error)` | Returns `ErrNotFound` if missing |
| `FindAll(ctx)` | `SELECT * FROM {table}` | `([]T, error)` | Returns `[]T{}` if empty, never nil |
| `FindByQuery(ctx, q)` | User-provided | `([]T, error)` | Custom query |
| `FindOneByQuery(ctx, q)` | User-provided | `(*T, error)` | Returns `nil, nil` if not found |
| `Count(ctx)` | `SELECT COUNT(*) FROM {table}` | `(int64, error)` | Row count |
| `Exists(ctx, id)` | `SELECT 1 FROM {table} WHERE id = :id` | `(bool, error)` | Existence check |
| `Insert(ctx, q)` | User-provided INSERT | `(sql.Result, error)` | Requires Query |
| `Update(ctx, q)` | User-provided UPDATE | `(sql.Result, error)` | Requires Query |
| `Delete(ctx, q)` | User-provided DELETE | `(sql.Result, error)` | Requires Query |
| `DeleteByID(ctx, id)` | `DELETE FROM {table} WHERE id = :id` | `error` | Returns `ErrNotFound` if 0 rows |

**Access methods:**
- `DB() *sql.DB` - Returns underlying connection
- `TableName() string` - Returns table name

### 4.2 Transaction Flow

```go
// Source: pkg/repository/transaction.go:17-47
func (r *Repository[T, ID]) WithTx(ctx context.Context, fn func(*TxRepository[T, ID]) error) error
```

**Flow:**
1. Begin transaction via `db.BeginTx(ctx, nil)`
2. Create `TxRepository` wrapping the transaction
3. Execute callback function
4. On error: rollback, return original error
5. On success: commit

**Example:**

```go
// Source: pkg/repository/transaction_test.go:12-31
err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
    q, _ := query.Build("INSERT INTO users ...", params)
    _, err := tx.Insert(ctx, q)
    return err  // nil = commit, error = rollback
})
```

### 4.3 Query Integration

All write operations require pre-built `*query.Query`:

```go
// Source: pkg/repository/repository_test.go:128-140
q, err := query.Build(
    "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
    map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
)
result, err := repo.Insert(ctx, q)
```

### 4.4 Error Handling

| Condition | Error |
|-----------|-------|
| `db == nil` | `ErrNilDB` |
| `FindByID` no row | `ErrNotFound` |
| `DeleteByID` no row | `ErrNotFound` |
| `FindOneByQuery` no row | `nil, nil` (no error) |
| Query is nil | `errors.New("query cannot be nil")` |

### 4.5 Wiring Map

| From | To | Trigger |
|------|-----|---------|
| `Repository.FindByID` | `query.Build` | Named param expansion |
| `Repository.FindByID` | `scan.Row[T]` | Row scanning |
| `Repository.FindAll` | `scan.All[T]` | Multi-row scanning |
| `Repository.FindOneByQuery` | `scan.One[T]` | Optional single row |
| `Repository.WithTx` | `TxRepository` | Transaction scope |

---

## 5. Testing

### 5.1 Test Patterns

```go
// Source: pkg/repository/repository_test.go:18-32
func setupTestDB(t *testing.T) *sql.DB {
    db, _ := sql.Open("turso", ":memory:")
    db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT, name TEXT)`)
    return db
}
```

### 5.2 Test Coverage

| Area | Tests |
|------|-------|
| CRUD | `TestNew`, `TestFindByID`, `TestFindAll`, `TestInsert`, `TestUpdate`, `TestDelete`, `TestDeleteByID` |
| Edge Cases | `TestFindByID_NotFound`, `TestFindAll_Empty`, `TestDeleteByID_NotFound` |
| Queries | `TestFindByQuery`, `TestFindOneByQuery`, `TestFindOneByQuery_NotFound` |
| Transactions | `TestWithTx_Commit`, `TestWithTx_Rollback`, `TestWithTx_NilDB` |
| Tx Operations | `TestTxRepository_*` (all CRUD within transaction) |
| Nil DB | `TestNilDB` (all methods with nil db) |

### 5.3 Concurrent Tests

```go
// Source: pkg/repository/benchmark_test.go:121-170
func TestConcurrentReads(t *testing.T)       // 10 goroutines, 50 iterations
func TestConcurrentWrites(t *testing.T)      // 5 goroutines, 20 iterations
func TestConcurrentTransactions(t *testing.T) // 5 goroutines, 10 iterations
```

SQLite-specific setup for concurrent tests:
- Uses temp file (not `:memory:`)
- `PRAGMA journal_mode=WAL`
- `PRAGMA busy_timeout=5000`
- `MaxOpenConns(1)` for single writer

### 5.4 Memory Test

```go
// Source: pkg/repository/benchmark_test.go:193-219
func TestMemoryLargeResultSet(t *testing.T) // 50k rows
```

---

## 6. Benchmarks

### 6.1 Available Benchmarks

```go
// Source: pkg/repository/benchmark_test.go
BenchmarkFindByID           // Single row lookup
BenchmarkFindAll_100        // 100 rows
BenchmarkFindAll_1000       // 1000 rows
BenchmarkFindAll_10000      // 10000 rows
BenchmarkInsert             // Single insert
BenchmarkUpdate             // Single update
BenchmarkTransaction        // Insert within transaction
```

---

## 10. Gaps and Issues

### 10.1 README/Code Mismatch

The README documents a different API than implemented:

| README Claims | Actual Implementation |
|---------------|----------------------|
| `Entity` interface with `TableName()`, `PrimaryKey()` | No interface, table name passed to `New()` |
| `New[T Entity](db)` | `New[T any, ID comparable](db, tableName)` |
| `Create(ctx, &entity)` | `Insert(ctx, query)` |
| `Update(ctx, &entity)` | `Update(ctx, query)` |
| `Delete(ctx, id)` | `Delete(ctx, query)` or `DeleteByID(ctx, id)` |
| `Transaction()` | `WithTx()` |

**Action needed:** Update README to match actual implementation.

### 10.2 Missing Features vs README

- [ ] No automatic field-to-column mapping for inserts/updates
- [ ] No automatic SQL generation from entity
- [ ] No pagination helpers

### 10.3 Design Decisions

The current implementation is intentionally lower-level:
- **Pro:** More flexible, explicit SQL
- **Pro:** No reflection overhead for writes
- **Con:** More boilerplate for simple CRUD
- **Con:** README sets wrong expectations

### 10.4 Technical Debt

- `ErrEmptyTableName` defined but never returned (no validation in `New`)
- Transaction isolation level hardcoded to `nil` (default)
- No context timeout support in transactions

### 10.5 Improvement Opportunities

1. Update README to document actual API
2. Add optional Entity interface for auto-CRUD if desired
3. Add `NewWithValidation` that checks table name
4. Add `WithTxOptions` for isolation level control
