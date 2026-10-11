# go-sqlite

Type-safe SQLite toolkit for Go.

## Features

- **Database** - One writer, read-only readers, WAL, and pure-Go SQLite
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

## Find Consumers

`go-sqlite-consumers` scans Go modules without changing them. It resolves the
latest published `go-sqlite` tag once, parses every `go.mod`, and separates
outdated pins from local and legacy replacements.

```bash
make consumer-build
./bin/go-sqlite-consumers scan ~/werk
./bin/go-sqlite-consumers scan ~/werk --json
./bin/go-sqlite-consumers scan ~/werk --check
./bin/go-sqlite-consumers scan ~/werk --target v0.8.0
```

`--check` exits non-zero when published pins are behind the target. Local
replacements are reported but are not treated as upgradeable pins. Use
`make consumer-scan ARGS="--json"` for the repository shortcut. Override its
root with `CONSUMER_ROOT=/path/to/workspace`.

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

Use `WithTx` on a repository built with `db.Writer()`. Writes wait for the
single writer connection in Go. Each write transaction starts with `BEGIN IMMEDIATE`.

```go
err := repo.WithTx(ctx, func(tx *repository.Repository[User, string]) error {
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

For lower-level operations, use `db.Write(ctx, func(tx *sql.Tx) error { ... })`.
Use only the supplied transaction inside the callback. A nil result commits.
An error or panic rolls back. The panic propagates.

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

v0.8.0 uses `modernc.org/sqlite v1.60.1`. File databases have one writer
connection and a read-only reader pool (four connections by default).
Transactions on the writer start with `BEGIN IMMEDIATE`, so a read followed
by an update has no lost updates. Writers wait in Go for the connection.

| Helper | Pool |
|---|---|
| `Query`, `QueryOne`, `Read` | Reader |
| `Exec`, `Write`, `BeginTx` | Writer (`ReadOnly: true` uses the reader) |
| `DB()`, `Writer()` | Writer |
| `Reader()` | Read-only reader pool |

`DB()` remains a generic handle. Repositories use the handle you supply.
Build read repositories with `db.Reader()` and write repositories with
`db.Writer()` to retain the split.

For `:memory:` and `file::memory:` databases, both roles share one connection.
Writes and reads see the same data. Close rows and transactions before another
operation needs that connection.

Every connection gets the configured PRAGMAs through the modernc DSN:

| PRAGMA | Default | Purpose |
|---|---|---|
| `journal_mode` | `WAL` | Reads and writes can proceed together |
| `synchronous` | `NORMAL` | Application-crash durability |
| `busy_timeout` | `5000` | Lock wait in milliseconds |
| `foreign_keys` | `ON` | Referential integrity |
| `temp_store` | `MEMORY` | Temporary tables in memory |
| `cache_size` | `-16000` | 16,000 KiB page cache per connection |

```go
db, err := database.Open(ctx, "app.db")

cfg := database.DefaultConfig().WithPragma("synchronous", "FULL")
db, err = database.Open(ctx, "app.db", database.WithConfig(cfg))
```

`MaxOpenConns`, `MaxIdleConns`, and `ConnMaxLifetime` configure the reader
pool. The writer always keeps one connection with no lifetime limit.
`DefaultConfig`, `ProductionConfig`, and `DevelopmentConfig` use the same WAL
baseline. `Open` copies the config. It preflights PRAGMAs in the requested order, then
adds the remaining names in sorted order. Strict mode aborts on PRAGMA errors.
Non-strict mode omits rejected PRAGMAs. The DSN reapplies accepted PRAGMAs on
each connection. Modernc determines their final order. See
[database](pkg/database/README.md) for complete config semantics.
SQLite supports `WITHOUT ROWID` tables.

### Write traffic and WAL

One process owns each file. Use `Write` for short transactions and `Queue`
for sustained traffic or FIFO service. The queue batches up to 64 operations
with a default wait of 1 ms. Each operation has its own savepoint.
`Queue.Submit` returns success after the batch commits. Close the queue before
the database to drain accepted operations.

`NORMAL` survives an application crash. Recent commits can be lost after an
OS crash or power loss. Use `FULL` when acknowledgements must survive power
loss, and use the queue to share the cost of each fsync.

Close every `Rows` and transaction promptly. Long reads pin the WAL.
Use `Read` for long reads and `Checkpoint(ctx, quiet)` from one timer to
create a quiet window. Repeated `busy` results mean readers still prevent
truncation. Direct reader queries bypass that window.

The [consumer guide](docs/consumer-guide.md) defines the supported envelope:
up to 1,000 sustained writes/s, brief bursts around 5,000/s, and write deadlines
around one second. Consumers impose deadlines and fast overload rejection.
Measure the real workload before relying on higher rates or larger transactions.
Evidence lives in [the writer lab](lab/sqlite-writers/README.md).

Prefer the owner's authenticated API or local admin socket for live inspection.
Plain `sqlite3` reads snapshots and live SQLite WAL databases safely in read-only
mode. Use SQLite backup or an owner-produced snapshot instead of copying an
open file. See [operations](docs/operations.md) and [agent access](docs/agent-api.md).

### Upgrading from v0.7.x

The API and storage format change together. Convert Turso-format files with
[cmd/turso-convert](cmd/turso-convert/) before the new service opens them.
Stop the owner, convert to a new file, verify it, then switch the service.
`Open` rejects an MVCC header or `-log` sidecar with a converter pointer.
The [upgrade guide](docs/consumer-guide.md#upgrading-from-v07x) maps removed APIs
and gives the conversion procedure. Historical `lab/turso-*` modules remain
on disk for recorded evidence. `make lab-test` runs the modernc labs.

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

## Examples

```bash
go run tmp/examples/repository/main.go
go run tmp/examples/transactions/main.go
go run tmp/examples/migrations/main.go
go run tmp/examples/timezones/main.go
```

## Documentation

Use the [documentation index](docs/readme.md) for consumer upgrades,
integration, agent access, and production-operation guidance.

## Requirements

- Go 1.26+

## License

MIT
