---
tags: [database, sqlite, turso, connection, pool]
status: implemented
owner: pkg/database/
integrations: [turso]
extracted_from: pkg/database/
---

# Database Specification

**Type:** Extraction
**Extracted From:** `pkg/database/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose
Wraps `database/sql` with configuration-based initialization, connection pooling, pragma support, and context propagation for SQLite/Turso databases.

### 1.2 Current Capabilities
- Open database connections with functional options
- Connection pool configuration (max open, idle, lifetime)
- SQLite pragma management
- Context-aware query execution
- Transaction support
- Thread-safe operations with RWMutex

### 1.3 Boundaries
- Uses only `turso` driver (hardcoded import)
- No automatic schema management
- No query building (see pkg/query)
- No row scanning helpers (see pkg/scan)

---

## 2. Architecture

### 2.1 Component Structure
```
pkg/database/
├── config.go         # Config type, preset configs, builder methods
├── database.go       # Database wrapper, Open, operations
├── errors.go         # Error types (QueryError, ExecError)
├── database_test.go  # Unit tests
└── README.md         # Package documentation
```

### 2.2 Component Diagram
```
┌─────────────────────┐
│     Application     │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│      Database       │ context-aware wrapper
├─────────────────────┤
│ - db: *sql.DB       │
│ - config: *Config   │
│ - path: string      │
│ - closed: bool      │
│ - mu: sync.RWMutex  │
└──────────┬──────────┘
           │
           ▼
┌─────────────────────┐
│    turso driver     │ turso.tech/database/tursogo
└─────────────────────┘
```

---

## 3. Core Types

### 3.1 Config
```go
// Source: pkg/database/config.go:1-12
type Config struct {
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
    Pragmas         map[string]string
}
```

### 3.2 Database
```go
// Source: pkg/database/database.go:11-19
type Database struct {
    db     *sql.DB
    config *Config
    path   string
    closed bool
    mu     sync.RWMutex
}
```

### 3.3 Option
```go
// Source: pkg/database/database.go:21-22
type Option func(*Database) error
```

### 3.4 Error Types
```go
// Source: pkg/database/errors.go:9-15
var (
    ErrClosed        = errors.New("database is closed")
    ErrInvalidPath   = errors.New("invalid database path")
    ErrInvalidConfig = errors.New("invalid configuration")
)

// Source: pkg/database/errors.go:17-27
type QueryError struct {
    Query string
    Err   error
}

// Source: pkg/database/errors.go:29-39
type ExecError struct {
    Query string
    Err   error
}
```

---

## 4. Data Flow

### 4.1 Connection Flow
```
Open(ctx, path, opts...)
    │
    ├── Validate path (non-empty)
    │
    ├── Apply functional options
    │
    ├── sql.Open("turso", path)
    │
    ├── Configure pool:
    │   ├── SetMaxOpenConns
    │   ├── SetMaxIdleConns
    │   └── SetConnMaxLifetime
    │
    ├── Apply pragmas (PRAGMA key = value)
    │   └── Errors ignored for compatibility
    │
    └── Ping to verify connection
```

### 4.2 Query Flow
```
db.Query(ctx, sql, args...)
    │
    ├── RLock mutex
    │
    ├── Check closed → ErrClosed
    │
    ├── db.QueryContext(ctx, query, args...)
    │   └── Error → QueryError{Query, Err}
    │
    └── Return *sql.Rows
```

### 4.3 Exec Flow
```
db.Exec(ctx, sql, args...)
    │
    ├── RLock mutex
    │
    ├── Check closed → ErrClosed
    │
    ├── db.ExecContext(ctx, query, args...)
    │   └── Error → ExecError{Query, Err}
    │
    └── Return sql.Result
```

### 4.4 Current Error Handling
- Empty path → `ErrInvalidPath`
- Nil config option → `ErrInvalidConfig`
- Operations on closed db → `ErrClosed`
- Query failures → wrapped in `QueryError`
- Exec failures → wrapped in `ExecError`
- Pragma failures → **silently ignored** (for compatibility)

### 4.5 Edge Case Behavior
- Double Close returns `ErrClosed`
- QueryOne on closed db: error surfaced at `Scan()` time
- Context cancellation propagated to underlying driver

### 4.6 Wiring Map
| From | To | Trigger |
|------|-----|---------|
| Open() | sql.Open() | Connection |
| Query() | db.QueryContext() | SELECT |
| QueryOne() | db.QueryRowContext() | SELECT single |
| Exec() | db.ExecContext() | INSERT/UPDATE/DELETE |
| BeginTx() | db.BeginTx() | Transaction start |
| Close() | db.Close() | Shutdown |

---

## 5. Configuration Presets

### 5.1 DefaultConfig
```go
// Source: pkg/database/config.go:14-24
MaxOpenConns:    25
MaxIdleConns:    5
ConnMaxLifetime: 5 * time.Minute
Pragmas: {
    "journal_mode": "WAL",
    "synchronous":  "NORMAL",
    "cache_size":   "-20000",  // 20MB
    "busy_timeout": "5000",
}
```

### 5.2 DevelopmentConfig
```go
// Source: pkg/database/config.go:26-37
MaxOpenConns:    10
MaxIdleConns:    2
ConnMaxLifetime: 1 * time.Minute
Pragmas: {
    "journal_mode": "DELETE",
    "synchronous":  "FULL",
    "cache_size":   "-5000",   // 5MB
    "busy_timeout": "3000",
}
```

### 5.3 ProductionConfig
```go
// Source: pkg/database/config.go:39-51
MaxOpenConns:    100
MaxIdleConns:    10
ConnMaxLifetime: 15 * time.Minute
Pragmas: {
    "journal_mode": "WAL",
    "synchronous":  "NORMAL",
    "cache_size":   "-64000",  // 64MB
    "busy_timeout": "10000",
    "foreign_keys": "ON",
}
```

### 5.4 Builder Methods
All return `*Config` for chaining:
- `WithMaxOpenConns(n int)`
- `WithMaxIdleConns(n int)`
- `WithConnMaxLifetime(d time.Duration)`
- `WithPragma(key, value string)`

---

## 6. API Surface

### 6.1 Open
```go
func Open(ctx context.Context, path string, opts ...Option) (*Database, error)
```
Opens database with turso driver. Applies config, pragmas, and pings.

### 6.2 WithConfig
```go
func WithConfig(cfg *Config) Option
```
Functional option to set custom configuration.

### 6.3 Database Methods
| Method | Signature | Returns |
|--------|-----------|---------|
| Query | `(ctx, query, args...) (*sql.Rows, error)` | Rows iterator |
| QueryOne | `(ctx, query, args...) *sql.Row` | Single row |
| Exec | `(ctx, query, args...) (sql.Result, error)` | Affected rows |
| BeginTx | `(ctx, opts) (*sql.Tx, error)` | Transaction |
| Close | `() error` | Close connection |
| Path | `() string` | Database path |
| Config | `() *Config` | Current config |
| DB | `() *sql.DB` | Underlying connection |

---

## 7. Usage Patterns

### 7.1 In-Memory Database
```go
db, err := database.Open(ctx, ":memory:")
```

### 7.2 Custom Configuration
```go
cfg := database.DefaultConfig().
    WithMaxOpenConns(50).
    WithPragma("foreign_keys", "ON")

db, err := database.Open(ctx, "file:./data.db", database.WithConfig(cfg))
```

### 7.3 Transaction
```go
tx, err := db.BeginTx(ctx, nil)
if err != nil {
    return err
}
defer tx.Rollback()

// operations...

return tx.Commit()
```

---

## 10. Gaps and Issues

### 10.1 Documentation Drift
- [ ] README documents `New(ctx, Config{Driver, Source})` API that doesn't exist
- [ ] README says "sqlite3" driver support but code only imports turso driver
- [ ] README shows `QueryRow()` but code has `QueryOne()`

### 10.2 Missing Features
- [ ] No sqlite3 driver option (hardcoded to turso)
- [ ] No connection health check beyond initial ping
- [ ] No prepared statement caching

### 10.3 Technical Debt
- Pragma application silently ignores all errors (could mask real issues)
- `QueryOne()` doesn't check closed state before calling QueryRowContext

### 10.4 Improvement Opportunities
- Add driver selection (sqlite3 vs turso)
- Add connection health monitoring
- Surface pragma errors selectively (ignore unsupported, report syntax)
- Add context to QueryOne closed check
