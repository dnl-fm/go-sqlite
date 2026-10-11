# migrations

Migrations register Go functions and run in version order. The runner records
successful versions in `_migrations`. A failed function stops the run.
Use the writer handle from `database.Open` so schema changes use the owner.
SQLite supports `WITHOUT ROWID` tables.

## Register and run

```go
import (
    "context"
    "database/sql"
    "github.com/dnl-fm/go-sqlite/pkg/database"
    "github.com/dnl-fm/go-sqlite/pkg/migrations"
)

func init() {
    migrations.Register(migrations.Migration{
        Version: "20261010000001",
        Description: "create_users",
        Up: func(ctx context.Context, db *sql.DB) error {
            _, err := db.ExecContext(ctx, `
                CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT NOT NULL UNIQUE)
            `)
            return err
        },
        Down: func(ctx context.Context, db *sql.DB) error {
            _, err := db.ExecContext(ctx, "DROP TABLE users")
            return err
        },
    })
}

func run(ctx context.Context) error {
    db, err := database.Open(ctx, "app.db")
    if err != nil {
        return err
    }
    defer db.Close()
    return migrations.Run(ctx, db.Writer())
}
```

Import the app's migration package before `Run` so its `init` functions register
the migrations. Use the app's module path for that import.

## API

| Function | Purpose |
|---|---|
| `Register(Migration)` | Register a version and its `Up`/`Down` functions |
| `Run(ctx, db)` | Run pending migrations in version order |
| `Rollback(ctx, db, version)` | Roll back the target and all later versions (empty target rolls back the latest) |
| `Status(ctx, db)` | Return registered versions and execution details |
| `Latest(ctx, db)` | Return the latest executed version |
| `Reset()` | Clear the registry for isolated tests |

`MigrationFunc` is `func(context.Context, *sql.DB) error`.
Each `Up` or `Down` function owns its transaction when atomic changes are necessary.
Migration tracking does not make all statements in that function atomic.
Use only the transaction handle for statements after `BeginTx`.

Keep migrations small. Exercise both directions with the app's schema.
Preserve deployed migrations and document any irreversible change.
Create a verified backup before production migrations.

## CLI

```bash
make build
export DATABASE_URL="file:./app.db"
./bin/migrate create add_users_table
./bin/migrate up
./bin/migrate status
./bin/migrate down
./bin/migrate down 20261010000001
```

`create` generates a Go migration file. Compile the app's registered migrations
into its migration runner. An unrelated binary cannot discover Go source files
at runtime. The URL names a local SQLite file, not a remote database endpoint.

See [quick start](QUICK_START.md), [database](../database/README.md), and the
[storage upgrade procedure](../../docs/consumer-guide.md#convert-deployed-data).
