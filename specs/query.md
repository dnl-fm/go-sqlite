---
tags: [query, sql, parameters, named-params]
status: implemented
owner: pkg/query/
integrations: []
extracted_from: pkg/query/
---

# Query Specification

**Type:** Extraction
**Extracted From:** `pkg/query/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose
Provides SQL query building with named parameters using `:name` placeholder syntax. Parameters are converted to `?` placeholders with ordered arguments for safe database execution.

### 1.2 Current Capabilities
- Build queries with named parameters (`:name` syntax)
- Convert to `?` placeholders with ordered args
- Validate all placeholders have values
- Detect unused parameters (typo prevention)
- Support duplicate placeholder usage
- Extract parameter names from SQL
- Validate parameter name syntax

### 1.3 Boundaries
- No query composition (no WHERE builders, no JOINs)
- No result set handling (see pkg/scan)
- No connection management (see pkg/database)
- Converts all queries to `?` style (no native named parameter support)

---

## 2. Architecture

### 2.1 Component Structure
```
pkg/query/
├── query.go           # Query type, Build, New functions
├── params.go          # ExtractParams, IsValidParamName helpers
├── query_test.go      # Unit tests
├── integration_test.go # Database integration tests
├── benchmark_test.go  # Performance benchmarks
└── README.md          # Package documentation
```

### 2.2 Component Diagram
```
┌─────────────────────────────┐
│        Application          │
└──────────────┬──────────────┘
               │
       Build() or New()
               │
               ▼
┌─────────────────────────────┐
│           Query             │
├─────────────────────────────┤
│ original: "... :name ..."   │
│ sql:      "... ? ..."       │
│ args:     []any{value}      │
│ params:   map[string]any    │
└──────────────┬──────────────┘
               │
    q.SQL(), q.Args()...
               │
               ▼
┌─────────────────────────────┐
│    database/sql execution   │
└─────────────────────────────┘
```

---

## 3. Core Types

### 3.1 Query
```go
// Source: pkg/query/query.go:24-29
type Query struct {
    original string         // Original SQL with :name placeholders
    sql      string         // Converted SQL with ? placeholders
    args     []any          // Ordered arguments matching ? positions
    params   map[string]any // Original named parameters
}
```

### 3.2 Errors
```go
// Source: pkg/query/query.go:12-18
var (
    ErrEmptySQL     = errors.New("query: SQL string cannot be empty")
    ErrMissingParam = errors.New("query: missing required parameter")
    ErrExtraParam   = errors.New("query: unused parameter provided")
)
```

### 3.3 Parameter Pattern
```go
// Source: pkg/query/query.go:21
var paramPattern = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)
```

Valid names: start with letter or `_`, followed by alphanumeric or `_`

---

## 4. Data Flow

### 4.1 Build Flow
```
Build(sql, params)
    │
    ├── Empty SQL? → ErrEmptySQL
    │
    ├── Nil params? → treat as empty map
    │
    ├── Find all :name placeholders (regex)
    │
    ├── Collect unique placeholder names
    │
    ├── Validate: all placeholders have values
    │   └── Missing → ErrMissingParam: name
    │
    ├── Validate: all params are used
    │   └── Unused → ErrExtraParam: name
    │
    ├── Build converted SQL:
    │   ├── strings.Builder for efficiency
    │   ├── Replace each :name with ?
    │   └── Append param value to args[]
    │
    └── Return *Query{original, sql, args, params}
```

### 4.2 New Flow (No Parameters)
```
New(sql)
    │
    ├── Empty SQL? → ErrEmptySQL
    │
    ├── Has :name placeholders?
    │   └── Yes → error "use Build() instead"
    │
    └── Return *Query{original: sql, sql: sql, args: [], params: {}}
```

### 4.3 Duplicate Placeholder Handling
Same parameter used multiple times:
```sql
SELECT * FROM users WHERE name = :search OR email LIKE :search
```
- One entry in `params`: `{"search": "value"}`
- Two entries in `args`: `["value", "value"]`
- Two `?` in converted SQL

### 4.4 Current Error Handling
| Condition | Error |
|-----------|-------|
| Empty SQL | `ErrEmptySQL` |
| Placeholder without value | `ErrMissingParam: name` |
| Param not in SQL | `ErrExtraParam: name` |
| Placeholder in New() | `"use Build() instead"` |

### 4.5 Wiring Map
| From | To | Trigger |
|------|-----|---------|
| Build() | paramPattern.FindAllStringSubmatchIndex() | Parse placeholders |
| Build() | strings.Builder | Construct converted SQL |
| New() | paramPattern.FindStringSubmatch() | Check for placeholders |
| ExtractParams() | paramPattern.FindAllStringSubmatch() | Extract names |

---

## 5. API Surface

### 5.1 Build
```go
// Source: pkg/query/query.go:31-87
func Build(sqlStr string, params map[string]any) (*Query, error)
```
Creates Query with named parameters converted to `?` placeholders.

**Example:**
```go
q, err := query.Build(
    "SELECT * FROM users WHERE email = :email AND active = :active",
    map[string]any{"email": "alice@test.com", "active": true},
)
rows, err := db.Query(q.SQL(), q.Args()...)
```

### 5.2 New
```go
// Source: pkg/query/query.go:89-108
func New(sqlStr string) (*Query, error)
```
Creates Query without parameters. Returns error if SQL contains `:name` placeholders.

**Example:**
```go
q, err := query.New("SELECT * FROM users")
rows, err := db.Query(q.SQL())
```

### 5.3 Query Methods
| Method | Returns | Description |
|--------|---------|-------------|
| `SQL()` | `string` | Converted SQL with `?` placeholders |
| `Args()` | `[]any` | Ordered arguments for execution |
| `Params()` | `map[string]any` | Original named parameters |
| `Original()` | `string` | Original SQL with `:name` placeholders |
| `String()` | `string` | Same as SQL() (implements Stringer) |

### 5.4 Helper Functions
```go
// Source: pkg/query/params.go:7-25
func ExtractParams(sqlStr string) []string
```
Returns all unique parameter names, sorted alphabetically.

```go
// Source: pkg/query/params.go:27-46
func IsValidParamName(name string) bool
```
Validates parameter name syntax (letter/underscore start, alphanumeric/underscore body).

---

## 6. Usage Patterns

### 6.1 INSERT
```go
q, _ := query.Build(
    "INSERT INTO users (name, email) VALUES (:name, :email)",
    map[string]any{"name": "Alice", "email": "alice@example.com"},
)
result, err := db.ExecContext(ctx, q.SQL(), q.Args()...)
```

### 6.2 SELECT with Multiple Params
```go
q, _ := query.Build(
    "SELECT * FROM users WHERE email = :email AND active = :active",
    map[string]any{"email": "test@example.com", "active": true},
)
rows, err := db.QueryContext(ctx, q.SQL(), q.Args()...)
```

### 6.3 Duplicate Placeholders
```go
q, _ := query.Build(
    "SELECT * FROM products WHERE category = :search OR name LIKE :search",
    map[string]any{"search": "Electronics"},
)
// Args: ["Electronics", "Electronics"]
```

### 6.4 Parameter Extraction
```go
sql := "SELECT * FROM users WHERE id = :id AND name = :name"
params := query.ExtractParams(sql) // ["id", "name"]
```

---

## 7. Test Coverage

### 7.1 Unit Tests (query_test.go)
| Test | Coverage |
|------|----------|
| `TestBuild` | Valid queries, empty SQL, missing/extra params, nil params |
| `TestBuild_ArgsOrder` | Argument ordering matches placeholder order |
| `TestNew` | Simple queries, empty SQL, accidental placeholders |
| `TestQuery_String` | Stringer implementation |
| `TestExtractParams` | Single/multiple/repeated params, underscore names |
| `TestIsValidParamName` | Valid/invalid name patterns |

### 7.2 Integration Tests (integration_test.go)
| Test | Coverage |
|------|----------|
| `TestIntegration_Insert` | INSERT with named params |
| `TestIntegration_Select` | SELECT with single row |
| `TestIntegration_Update` | UPDATE with verification |
| `TestIntegration_Delete` | DELETE with count check |
| `TestIntegration_DuplicatePlaceholders` | Same param used twice |
| `TestIntegration_New` | Query without params |

### 7.3 Benchmarks (benchmark_test.go)
| Benchmark | Purpose |
|-----------|---------|
| `BenchmarkBuild_Simple` | Single param query |
| `BenchmarkBuild_MultipleParams` | 3 params |
| `BenchmarkBuild_ManyParams` | 10 params |
| `BenchmarkBuild_RepeatedParams` | Same param 3 times |
| `BenchmarkNew` | No-param query |
| `BenchmarkExtractParams` | Param extraction |
| `BenchmarkQuery_SQL` | SQL accessor |
| `BenchmarkQuery_Args` | Args accessor |

---

## 10. Gaps and Issues

### 10.1 Documentation Drift
- [x] README documents `BuildDirect()` and `BuildConverted()` APIs that don't exist in code
- [x] README documents `DIRECT` and `CONVERTED` modes that don't exist
- [x] README documents `UnusedParams()` helper that doesn't exist
- [x] README documents `Validate()` method that doesn't exist
- [x] README shows `ErrInvalidParamName` and `ErrNoParams` errors that don't exist
- [x] README shows `Mode()` method that doesn't exist
- [ ] Benchmark in integration_test.go duplicates benchmark_test.go functionality

### 10.2 Missing Features
- [ ] No `sql.Named()` mode (DIRECT) for drivers that support it natively
- [ ] No query caching (same SQL rebuilt each time)
- [ ] No batch parameter binding
- [ ] No IN clause expansion (`:ids` → `?, ?, ?`)

### 10.3 Technical Debt
- None significant

### 10.4 Improvement Opportunities
- Add DIRECT mode using `sql.Named()` for drivers that support it
- Add parameter type validation (optional)
- Add query caching based on original SQL
- Add IN clause expansion helper
