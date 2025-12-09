# Project Context: go-turso-kit

SQLite toolkit for Go with Turso database support. Ported from TypeScript [bun-sqlite](https://github.com/fightbulc/bun-sqlite).

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                        Application                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐              │
│  │ Repository  │  │   Query     │  │    Zeit     │              │
│  │  + TxRepo   │  │  Builder    │  │  Timezone   │              │
│  └──────┬──────┘  └──────┬──────┘  └─────────────┘              │
│         │                │                                       │
│         │    ┌───────────┘                                       │
│         │    │                                                   │
│         ▼    ▼                                                   │
│  ┌─────────────────┐     ┌─────────────┐                        │
│  │      Scan       │     │ Migrations  │                        │
│  │  (struct tags)  │     │  (up/down)  │                        │
│  └────────┬────────┘     └──────┬──────┘                        │
│           │                     │                                │
│           └──────────┬──────────┘                                │
│                      ▼                                           │
│              ┌─────────────┐                                     │
│              │  Database   │                                     │
│              │  (sql.DB)   │                                     │
│              └──────┬──────┘                                     │
│                     │                                            │
├─────────────────────┼────────────────────────────────────────────┤
│                     ▼                                            │
│              ┌─────────────┐                                     │
│              │  turso-go   │                                     │
│              │   driver    │                                     │
│              └──────┬──────┘                                     │
│                     │                                            │
│                     ▼                                            │
│         ┌─────────────────────┐                                  │
│         │  SQLite / Turso DB  │                                  │
│         └─────────────────────┘                                  │
└─────────────────────────────────────────────────────────────────┘
```

## Package Structure

```
pkg/
├── database/       # Connection wrapper with pragmas
├── query/          # Named parameter queries (:name → ?)
├── repository/     # Generic CRUD with transactions
├── scan/           # Struct scanning from sql.Rows
├── migrations/     # Schema versioning (up/down)
├── id/
│   ├── ulid/       # Time-sortable IDs
│   └── nanoid/     # Compact random IDs
└── zeit/           # Timezone-aware datetime
```

## Core Patterns

### Query Flow

```
┌──────────────────────────────────────────────────────────────┐
│                      Query Building                           │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Build query with named params                             │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ query.Build(                                         │  │
│     │   "SELECT * FROM users WHERE email = :email",        │  │
│     │   map[string]any{"email": "alice@test.com"},         │  │
│     │ )                                                    │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  2. Validation                                                │
│     • Check all :placeholders have values                     │
│     • Check no unused params (catch typos)                    │
│     • Convert :name → ? with ordered args                     │
│                            │                                  │
│                            ▼                                  │
│  3. Execute                                                   │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ db.Query(q.SQL(), q.Args()...)                       │  │
│     │                                                      │  │
│     │ SQL:  "SELECT * FROM users WHERE email = ?"          │  │
│     │ Args: ["alice@test.com"]                             │  │
│     └─────────────────────────────────────────────────────┘  │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

### Repository + Scan Flow

```
┌──────────────────────────────────────────────────────────────┐
│                    Repository Pattern                         │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Define entity with db tags                                │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ type User struct {                                   │  │
│     │     ID    string `db:"id"`                           │  │
│     │     Email string `db:"email"`                        │  │
│     │     Name  string `db:"name"`                         │  │
│     │ }                                                    │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  2. Create repository (no mapper needed)                      │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ repo := repository.New[User, string](db, "users")    │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  3. Query returns *sql.Rows                                   │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ rows, _ := db.Query("SELECT * FROM users")           │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  4. Scan package maps columns → struct fields                 │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ scan.All[User](rows)                                 │  │
│     │                                                      │  │
│     │ • Get column names from rows.Columns()               │  │
│     │ • Match to struct fields via `db` tag                │  │
│     │ • Column order doesn't matter                        │  │
│     │ • Unmapped columns ignored                           │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  5. Returns typed result                                      │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ []User{{ID: "1", Email: "...", Name: "..."}, ...}    │  │
│     └─────────────────────────────────────────────────────┘  │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

### Transaction Flow

```
┌──────────────────────────────────────────────────────────────┐
│                    Transaction Handling                       │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  repo.WithTx(ctx, func(tx *TxRepository) error {              │
│      ...                                                      │
│  })                                                           │
│                                                               │
│  ┌────────────────┐                                           │
│  │  Begin TX      │                                           │
│  └───────┬────────┘                                           │
│          │                                                    │
│          ▼                                                    │
│  ┌────────────────┐                                           │
│  │  Execute fn()  │                                           │
│  └───────┬────────┘                                           │
│          │                                                    │
│          ▼                                                    │
│     ┌─────────┐                                               │
│     │ error?  │                                               │
│     └────┬────┘                                               │
│          │                                                    │
│    ┌─────┴─────┐                                              │
│    │           │                                              │
│    ▼           ▼                                              │
│  ┌────┐     ┌────────┐                                        │
│  │ no │     │  yes   │                                        │
│  └──┬─┘     └───┬────┘                                        │
│     │           │                                             │
│     ▼           ▼                                             │
│  ┌────────┐  ┌──────────┐                                     │
│  │ COMMIT │  │ ROLLBACK │                                     │
│  └────────┘  └──────────┘                                     │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

### Migration Flow

```
┌──────────────────────────────────────────────────────────────┐
│                    Migration System                           │
├──────────────────────────────────────────────────────────────┤
│                                                               │
│  1. Register migrations                                       │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ migrations.Register(Migration{                       │  │
│     │     Version:     "20251107000001",                   │  │
│     │     Description: "create_users_table",               │  │
│     │     Up:          upFunc,                             │  │
│     │     Down:        downFunc,                           │  │
│     │ })                                                   │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│  2. Run migrations                                            │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ migrations.Run(ctx, db)                              │  │
│     └─────────────────────────────────────────────────────┘  │
│                            │                                  │
│                            ▼                                  │
│     ┌─────────────────────────────────────────────────────┐  │
│     │  _migrations table                                   │  │
│     │  ┌─────────────────┬─────────────────┬────────────┐ │  │
│     │  │ version         │ description     │ executed_at│ │  │
│     │  ├─────────────────┼─────────────────┼────────────┤ │  │
│     │  │ 20251107000001  │ create_users    │ 2025-12-09 │ │  │
│     │  │ 20251107000002  │ create_posts    │ 2025-12-09 │ │  │
│     │  └─────────────────┴─────────────────┴────────────┘ │  │
│     └─────────────────────────────────────────────────────┘  │
│                                                               │
│  3. Rollback (optional)                                       │
│     ┌─────────────────────────────────────────────────────┐  │
│     │ migrations.Rollback(ctx, db, "")  // last one        │  │
│     │ migrations.Rollback(ctx, db, "20251107000001")       │  │
│     └─────────────────────────────────────────────────────┘  │
│                                                               │
└──────────────────────────────────────────────────────────────┘
```

## Key Design Decisions

### 1. Named Parameters → Positional

turso-go doesn't support `sql.Named()`. We convert `:name` syntax to `?` placeholders:

```go
// Input
"SELECT * FROM users WHERE email = :email AND active = :active"
params: {"email": "a@b.com", "active": true}

// Output
SQL:  "SELECT * FROM users WHERE email = ? AND active = ?"
Args: ["a@b.com", true]  // ordered by appearance
```

### 2. Struct Tags for Scanning

No manual row mappers. Use `db` tags:

```go
type User struct {
    ID    string `db:"id"`
    Email string `db:"email"`
}

// Automatic mapping, column order independent
users, _ := scan.All[User](rows)
```

### 3. Generic Repository

Type-safe without code generation:

```go
// Repository[EntityType, IDType]
repo := repository.New[User, string](db, "users")
user, err := repo.FindByID(ctx, "user_123")
```

### 4. Zeit for Timezones

Store UTC timestamps, display in user timezone:

```go
z := zeit.Now(userTimezone)
dbTimestamp := z.ToDatabase()  // int64 UTC
userDisplay := z.ToUser()       // ISO string in user TZ
```

## File Map

| File | Purpose |
|------|---------|
| `pkg/database/database.go` | Connection wrapper, pragmas |
| `pkg/database/config.go` | Connection pool settings |
| `pkg/query/query.go` | `Build()`, `New()` functions |
| `pkg/query/params.go` | `ExtractParams()`, validation |
| `pkg/repository/repository.go` | Generic CRUD operations |
| `pkg/repository/transaction.go` | `WithTx()`, `TxRepository` |
| `pkg/scan/scan.go` | `Row()`, `All()`, `One()` |
| `pkg/migrations/migrations.go` | `Register()`, `Run()`, `Rollback()` |
| `pkg/id/ulid/ulid.go` | Time-sortable IDs |
| `pkg/id/nanoid/nanoid.go` | Compact random IDs |
| `pkg/zeit/zeit.go` | Timezone-aware datetime |
| `pkg/zeit/cycles.go` | Billing period generation |
| `pkg/zeit/duration.go` | Duration calculations |

## Test Coverage

| Package | Coverage | Tests |
|---------|----------|-------|
| `pkg/query` | 98.2% | 20 |
| `pkg/id/nanoid` | 93.8% | 24 |
| `pkg/zeit` | 93.4% | 44 |
| `pkg/id/ulid` | 91.5% | 22 |
| `pkg/database` | 90.4% | 20 |
| `pkg/scan` | 80.4% | 8 |
| `pkg/migrations` | 79.6% | 7 |
| `pkg/repository` | 77.2% | 40 |

Run tests:
```bash
go test ./pkg/... -cover
```

## Examples

Located in `tmp/examples/`:

| Example | Demonstrates |
|---------|--------------|
| `repository/` | CRUD, struct tags, custom queries |
| `transactions/` | Commit, rollback, isolation |
| `migrations/` | Schema versioning, up/down |
| `timezones/` | Zeit, business days, billing cycles |

Run:
```bash
go run tmp/examples/repository/main.go
```

## Common Tasks

### Add new entity

```go
// 1. Define struct with db tags
type Post struct {
    ID        string `db:"id"`
    Title     string `db:"title"`
    Content   string `db:"content"`
    AuthorID  string `db:"author_id"`
    CreatedAt int64  `db:"created_at"`
}

// 2. Create repository
postRepo := repository.New[Post, string](db, "posts")

// 3. Use it
posts, _ := postRepo.FindAll(ctx)
```

### Custom query

```go
q, _ := query.Build(
    `SELECT p.*, u.name as author_name 
     FROM posts p 
     JOIN users u ON p.author_id = u.id 
     WHERE p.created_at > :since`,
    map[string]any{"since": timestamp},
)
rows, _ := db.Query(q.SQL(), q.Args()...)
```

### Add migration

```go
migrations.Register(migrations.Migration{
    Version:     "20251210000001",
    Description: "add_posts_table",
    Up: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `
            CREATE TABLE posts (
                id TEXT PRIMARY KEY,
                title TEXT NOT NULL,
                content TEXT,
                author_id TEXT REFERENCES users(id),
                created_at INTEGER NOT NULL
            )
        `)
        return err
    },
    Down: func(ctx context.Context, db *sql.DB) error {
        _, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS posts`)
        return err
    },
})
```
