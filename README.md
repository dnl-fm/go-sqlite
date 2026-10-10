# go-sqlite

Type-safe SQLite toolkit for Go.

## Features

- **Database** - Connection wrapper with Turso MVCC, foreign keys, and busy timeout
- **Named Parameter Queries** - SQL with `:name` placeholders
- **Repository Pattern** - Generic CRUD with automatic struct scanning
- **Migrations** - Schema versioning with up/down functions
- **Transactions** - Automatic commit/rollback handling
- **Zeit** - Timezone-aware datetime, unix storage, billing cycles
- **ID Generation** - ULID (time-sortable) and NanoID (compact)

## Installation

```bash
go get github.com/dnl-fm/go-sqlite
```


## Quick Start

### 1. Define Your Entity

Use `db` struct tags to map columns:

```go
type User struct {
    ID    string `db:"id"`
    Email string `db:"email"`
    Name  string `db:"name"`
}
```

### 2. Create Repository

```go
import (
    "context"
    "github.com/dnl-fm/go-sqlite/pkg/database"
    "github.com/dnl-fm/go-sqlite/pkg/repository"
)

db, _ := database.Open(ctx, "app.db")
defer db.Close()

repo := repository.New[User, string](db.DB(), "users")
```

### 3. CRUD Operations

```go
ctx := context.Background()

// Find by ID
user, err := repo.FindByID(ctx, "user_123")

// Find all
users, err := repo.FindAll(ctx)

// Count
count, err := repo.Count(ctx)

// Check existence
exists, err := repo.Exists(ctx, "user_123")
```

### 4. Named Parameter Queries

```go
import "github.com/dnl-fm/go-sqlite/pkg/query"

q, err := query.Build(
    "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
    map[string]any{
        "id":    "user_123",
        "email": "alice@example.com",
        "name":  "Alice",
    },
)
repo.Insert(ctx, q)

// Custom SELECT
q, _ := query.Build(
    "SELECT * FROM users WHERE email LIKE :pattern AND active = :active",
    map[string]any{"pattern": "%@example.com", "active": true},
)
users, err := repo.FindByQuery(ctx, q)
```

### 5. Transactions

Use `WithConcurrentTx` for Turso MVCC write transactions. It runs
`BEGIN CONCURRENT` on a reserved connection so overlapping writes on different
pooled connections can proceed inside the owning process.

```go
err := repo.WithConcurrentTx(ctx, func(tx *repository.Repository[User, string]) error {
    q1, _ := query.Build(
        "INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
        map[string]any{"id": "1", "email": "alice@example.com", "name": "Alice"},
    )
    _, err := tx.Insert(ctx, q1)
    if err != nil {
        return err  // triggers rollback
    }
    return nil  // triggers commit
})
```

`WithTx` still exists for portable `database/sql` transactions, but it is deprecated for Turso MVCC write concurrency because it starts a regular `BEGIN`.

### 6. Migrations

```go
import "github.com/dnl-fm/go-sqlite/pkg/migrations"

// Run pending migrations on startup
migrations.Run(ctx, db.DB())

// Check status
statuses, _ := migrations.Status(ctx, db.DB())

// Rollback last migration
migrations.Rollback(ctx, db.DB(), "")
```

### 7. ID Generation

```go
import (
    "github.com/dnl-fm/go-sqlite/pkg/id/ulid"
    "github.com/dnl-fm/go-sqlite/pkg/id/nanoid"
)

// ULID - time-sortable, 26 chars
id := ulid.New()                           // 01ARZ3NDEKTSV4RRFFQ69G5FAV
id := ulid.NewWithPrefix("user_")          // user_01ARZ3NDEKTSV4RRFFQ69G5FAV
ts := id.Time()                            // extract creation time

// NanoID - compact, 21 chars (configurable)
id := nanoid.New()                         // V1StGXR8_Z5jdHi6B-myT
id := nanoid.NewWithLength(10)             // shorter ID
```

### 8. Timezone-Aware Dates

```go
import "github.com/dnl-fm/zeit-go"

// Current time in timezone
tokyo, _ := time.LoadLocation("Asia/Tokyo")
z := zeit.Now(tokyo)

// Database storage (UTC Unix timestamp)
dbValue := z.ToDatabase()  // int64
restored := zeit.FromDatabase(dbValue, tokyo)

// Automatic via sql.Scanner/driver.Valuer (use *Zeit in structs)
type Order struct {
    ID        string     `db:"id"`
    CreatedAt *zeit.Zeit `db:"created_at"`  // scans as UTC int64
}
order, _ := repo.FindByID(ctx, "order_123")
order.CreatedAt.In(appTZ).ToUser()  // "2024-01-15T11:30:00+01:00"

// Date arithmetic
tomorrow := z.AddDays(1)
nextBusinessDay := z.AddBusinessDays(1)  // skips weekends

// Billing cycles
cycles := z.Cycles(12, zeit.Monthly)
```

## Database Configuration

`database.Open` always opens through the Turso driver and enables MVCC. There is
no supported old-WAL mode in this package.

| PRAGMA | Default | Purpose |
|--------|---------|---------|
| `journal_mode` | `'mvcc'` | Concurrent writes across pooled connections |
| `synchronous` | `NORMAL` | Balanced durability/speed |
| `foreign_keys` | `ON` | Enforce referential integrity |
| `busy_timeout` | `1000` | Wait before locked error |
| `temp_store` | `MEMORY` | Temp tables in RAM |
| `cache_size` | `-64000` | 64MB page cache |
| `mmap_size` | `67108864` | 64MB memory-mapped I/O |

```go
// Default config (recommended)
db, _ := database.Open(ctx, "app.db")

// ProductionConfig is the same MVCC baseline.
db, _ := database.Open(ctx, "app.db",
    database.WithConfig(database.ProductionConfig()),
)

// Custom pragmas
cfg := database.DefaultConfig().WithPragma("cache_size", "-64000")
db, _ := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

Turso 0.7 makes MVCC and multiprocess WAL mutually exclusive. This package
chooses MVCC: one application process owns each database file, while its pooled
connections provide same-process concurrency. `EnableMultiprocessWAL` and
`WithMultiprocessWAL` remain for source compatibility, but `Open` rejects them.

Agents and CLI tools read live state through the owning app's authenticated API
or local admin socket. For offline forensics, inspect an owner-produced snapshot
or a cleanly closed database with `tursodb`; never raw-copy a live Turso file.
See [Turso Operations](docs/turso-operations.md) and the
[consumer agent-access contract](docs/agent-api.md).

### Turso MVCC

Turso 0.6.0 added experimental plain-engine `WITHOUT ROWID` support behind `?experimental=without_rowid`, and the 0.6.1 / 0.7 prerelease / stable 0.7.0 through 0.7.2 labs keep the same boundary: MVCC still rejects writes to those tables. This package requires normal rowid tables. `database.Open` validates existing schema, `database.Exec` rejects `WITHOUT ROWID` SQL, and the migration runner validates the schema after each migration. Turso 0.7.2 also fixes MVCC sequence bookkeeping leaking into user-visible change counts; the 0.7.2 lab proves `AUTOINCREMENT` reports user-row changes only. Rebuild existing plain SQLite databases into normal rowid tables before opening them with this package. The current probes live in `lab/turso-v060`, `lab/turso-v061`, `lab/turso-v070-pre2`, `lab/turso-v070-pre4`, `lab/turso-v070-pre5`, `lab/turso-v070-pre6`, `lab/turso-v070-pre10`, `lab/turso-v070-pre13`, `lab/turso-v070`, `lab/turso-v071`, and `lab/turso-v072`.

Install `tursodb` anywhere operators or tests run or inspect database files:

```bash
curl --proto '=https' --tlsv1.2 -LsSf \
  https://github.com/tursodatabase/turso/releases/latest/download/turso_cli-installer.sh | sh
source "$HOME/.turso/env"
```

Use `tursodb` instead of system `sqlite3` for offline inspection. It can read
normal SQLite databases and is required for Turso-format databases; plain
`sqlite3` can reject valid Turso files as "file is not a database". Do not open
the canonical file while its MVCC owner is running. Ask the service to produce
a snapshot, or stop it cleanly first.

```bash
tursodb snapshot.db "PRAGMA integrity_check;"
tursodb snapshot.db ".tables"
```

Process layout matters. One Go process can own one database file and use MVCC
with a normal connection pool. Many processes can write in parallel when each
process owns a different file, because there is no shared WAL file to lock.
Several Go writer processes against the same physical file are not a supported
shape here; route those writes through one owner process, split the files, or
put an app-level queue in front of the file.

For lower-level code, use `database.ConcurrentTx` or `database.ConcurrentTxRetry`:

```go
err := database.ConcurrentTxRetry(ctx, db.DB(), 5, func(tx database.ConnTx) error {
    _, err := tx.ExecContext(ctx, "INSERT INTO hits (val) VALUES (?)", 1)
    return err
})
```

## Migrations CLI

```bash
# Build
make build  # creates bin/migrate

# Usage
export DATABASE_URL="file:./app.db"
./bin/migrate create add_users_table
./bin/migrate up
./bin/migrate status
./bin/migrate down
```

Migration files register via `init()`:

```go
package migrations

import (
    "context"
    "database/sql"
    "github.com/dnl-fm/go-sqlite/pkg/migrations"
)

func init() {
    migrations.Register(migrations.Migration{
        Version:     "20251210123456",
        Description: "add_users_table",
        Up:          up20251210123456,
        Down:        down20251210123456,
    })
}

func up20251210123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `
        CREATE TABLE users (
            id TEXT PRIMARY KEY,
            email TEXT UNIQUE NOT NULL,
            created_at INTEGER NOT NULL DEFAULT 0
        )
    `)
    return err
}

func down20251210123456(ctx context.Context, db *sql.DB) error {
    _, err := db.ExecContext(ctx, `DROP TABLE users`)
    return err
}
```

## Package Overview

| Package | Description |
|---------|-------------|
| `pkg/database` | Connection wrapper with PRAGMAs and pooling |
| `pkg/query` | Named parameter query building |
| `pkg/repository` | Generic CRUD operations |
| `pkg/scan` | Automatic struct scanning from sql.Rows |
| `pkg/migrations` | Schema versioning |
| `pkg/id/ulid` | Time-sortable unique IDs |
| `pkg/id/nanoid` | Compact random IDs |
| [`zeit-go`](https://github.com/dnl-fm/zeit-go) | Timezone handling and billing cycles (external) |
| `pkg/driver/turso` | Turso database/sql driver registration |

## Examples

```bash
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```


## Requirements

- Go 1.26+

## License

MIT
