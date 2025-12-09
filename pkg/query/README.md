# Query Package

The `query` package provides a flexible query builder with named parameter support for SQLite databases using turso-go driver.

## Features

- **Two processing modes:**
  - `DIRECT`: Uses `sql.Named()` - zero conversion overhead (recommended)
  - `CONVERTED`: Converts `:name` syntax to `?` placeholders with ordered args
- **Named parameter validation**
- **Duplicate placeholder support** (same parameter used multiple times)
- **Helper functions** for parameter extraction and validation
- **100% test coverage** with comprehensive unit and integration tests

## Installation

```go
import "github.com/fightbulc/go-turso-kit/pkg/query"
```

## Quick Start

### DIRECT Mode (Recommended)

Uses `sql.Named()` to wrap parameters - no conversion overhead:

```go
q, err := query.BuildDirect(
    "SELECT * FROM users WHERE id = :id AND name = :name",
    map[string]any{"id": 1, "name": "John"},
)
if err != nil {
    // handle error
}

rows, err := db.QueryContext(ctx, q.String(), q.Args()...)
```

### CONVERTED Mode

Converts `:name` syntax to `?` placeholders with ordered arguments:

```go
q, err := query.BuildConverted(
    "INSERT INTO users (name, email) VALUES (:name, :email)",
    map[string]any{"name": "Jane", "email": "jane@example.com"},
)
if err != nil {
    // handle error
}

result, err := db.ExecContext(ctx, q.String(), q.Args()...)
```

## API Reference

### Core Types

```go
type Query struct {
    // Contains query, arguments, and metadata
}

type Mode int
const (
    DIRECT    Mode = iota  // Uses sql.Named()
    CONVERTED               // Converts :name → ?
)
```

### Building Queries

#### `BuildDirect(sql string, params map[string]any) (*Query, error)`

Creates a Query using `sql.Named()` for parameter wrapping.

**Example:**
```go
q, err := query.BuildDirect(
    "SELECT * FROM users WHERE age > :min_age",
    map[string]any{"min_age": 18},
)
```

**Returns:**
- `*Query`: Query object with wrapped parameters
- `error`: `ErrEmptySQL` or `ErrNoParams` if invalid

#### `BuildConverted(sql string, params map[string]any) (*Query, error)`

Creates a Query by converting `:name` syntax to `?` placeholders.

**Example:**
```go
q, err := query.BuildConverted(
    "UPDATE users SET name = :name WHERE id = :id",
    map[string]any{"name": "Bob", "id": 5},
)
```

**Returns:**
- `*Query`: Query object with converted SQL and ordered args
- `error`: `ErrMissingParam` if a required parameter is not provided

### Query Methods

#### `Args() []any`

Returns query arguments ready for `sql.DB` methods.

```go
db.ExecContext(ctx, q.String(), q.Args()...)
```

#### `String() string`

Returns SQL string ready for execution:
- DIRECT mode: Returns original SQL (params wrapped in Args)
- CONVERTED mode: Returns parsed SQL with `?` placeholders

#### `SQL() string`

Returns the original SQL string (with `:name` placeholders).

#### `Params() map[string]any`

Returns the original parameter map.

#### `Mode() Mode`

Returns the processing mode (DIRECT or CONVERTED).

#### `Validate() error`

Validates the query structure. Checks:
- SQL is not empty
- Parameters map is not nil

### Helper Functions

#### `ExtractParams(sql string) []string`

Extracts all unique parameter names from SQL string.

**Example:**
```go
params := query.ExtractParams("SELECT * WHERE id = :id AND name = :name")
// Returns: ["id", "name"] (sorted)
```

#### `UnusedParams(sql string, params map[string]any) []string`

Returns parameter names that were provided but not used in SQL.

**Example:**
```go
sql := "SELECT * WHERE id = :id"
params := map[string]any{"id": 1, "name": "John", "email": "john@example.com"}
unused := query.UnusedParams(sql, params)
// Returns: ["email", "name"] (sorted)
```

#### `IsValidParamName(name string) bool`

Checks if a parameter name is valid.

**Rules:**
- Must start with letter (a-z, A-Z) or underscore (_)
- Followed by letters, digits (0-9), or underscores

**Example:**
```go
query.IsValidParamName("user_id")    // true
query.IsValidParamName("_private")   // true
query.IsValidParamName("firstName")  // true
query.IsValidParamName("123invalid") // false
query.IsValidParamName("with-dash")  // false
```

## Error Types

```go
var (
    ErrEmptySQL         error  // SQL string is empty
    ErrMissingParam     error  // Required parameter not provided
    ErrInvalidParamName error  // Parameter name is invalid
    ErrNoParams         error  // Parameters map is nil
)
```

## Advanced Usage

### Duplicate Placeholders

Same parameter can be used multiple times:

```go
q, err := query.BuildConverted(
    "SELECT * FROM users WHERE name = :search OR email LIKE :search",
    map[string]any{"search": "John%"},
)
// Converted SQL: "SELECT * FROM users WHERE name = ? OR email LIKE ?"
// Args: ["John%", "John%"]
```

### Complex Queries

Works with subqueries, joins, and complex SQL:

```go
q, err := query.BuildConverted(
    `SELECT u.* FROM users u
     JOIN posts p ON u.id = p.user_id
     WHERE u.status = :status
     AND p.created_at > :date
     AND u.id IN (SELECT user_id FROM subscriptions WHERE active = :active)`,
    map[string]any{
        "status": "active",
        "date":   "2024-01-01",
        "active": true,
    },
)
```

### Validation Workflow

```go
sql := "SELECT * FROM users WHERE id = :id AND name = :name"
params := map[string]any{"id": 1, "name": "John", "extra": "unused"}

// Check for unused parameters
unused := query.UnusedParams(sql, params)
if len(unused) > 0 {
    log.Printf("Warning: unused parameters: %v", unused)
}

// Build query
q, err := query.BuildConverted(sql, params)
if err != nil {
    return err
}

// Validate query
if err := q.Validate(); err != nil {
    return err
}

// Execute
rows, err := db.QueryContext(ctx, q.String(), q.Args()...)
```

## Performance

Benchmarks on AMD RYZEN AI MAX+ 395:

```
BenchmarkBuildDirect-32         12,045,352    96.66 ns/op   144 B/op   4 allocs/op
BenchmarkBuildConverted-32       2,203,262   543.4 ns/op    663 B/op   9 allocs/op
BenchmarkExtractParams-32        2,023,771   593.3 ns/op    565 B/op  10 allocs/op
```

**DIRECT mode is ~5.6x faster** than CONVERTED mode. Use DIRECT mode unless you need the conversion features or query caching.

## Testing

The package includes comprehensive tests:

- **Unit tests**: `query_test.go` - 100% code coverage
- **Integration tests**: `integration_test.go` - Real database tests
- **Benchmarks**: Performance comparisons

Run tests:
```bash
go test -v ./pkg/query/
go test -bench=. -benchmem ./pkg/query/
go test -cover ./pkg/query/
```

## Examples

See `examples/query/main.go` for complete working examples demonstrating:
- DIRECT and CONVERTED modes
- INSERT, SELECT, UPDATE operations
- Duplicate placeholders
- Helper function usage
- Parameter validation

Run example:
```bash
go run examples/query/main.go
```

## Best Practices

1. **Use DIRECT mode** by default for best performance
2. **Use CONVERTED mode** when you need query string caching or want explicit control over arg ordering
3. **Validate parameters** using `UnusedParams()` in development to catch typos
4. **Check errors** from `Build*()` functions for missing parameters
5. **Use context** with all database operations for proper cancellation
6. **Avoid string concatenation** for SQL - always use parameterized queries

## License

Part of go-turso-kit project. See LICENSE for details.
