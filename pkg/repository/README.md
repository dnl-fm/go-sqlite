# repository

Repositories scan rows into structs and execute parameterized queries.
The type parameters describe the entity and its ID. Entities use `db` tags.
No entity interface is necessary.

```go
import (
    "github.com/dnl-fm/go-sqlite/pkg/query"
    "github.com/dnl-fm/go-sqlite/pkg/repository"
)

type User struct {
    ID    string `db:"id"`
    Email string `db:"email"`
}

readRepo := repository.New[User, string](db.Reader(), "users")
writeRepo := repository.New[User, string](db.Writer(), "users")
user, err := readRepo.FindByID(ctx, "user_123")

q, err := query.Build(
    "INSERT INTO users (id, email) VALUES (:id, :email)",
    map[string]any{"id": "user_123", "email": "alice@example.com"},
)
if err != nil {
    return err
}
_, err = writeRepo.Insert(ctx, q)
```

The repository retains the `DBTX` handle you supply (`*sql.DB` or `*sql.Tx`).
It does not route reads and writes itself. A repository built with `db.DB()`
uses the writer for every operation. A reader repository rejects writes.

## Operations

| Method | Result |
|---|---|
| `FindByID(ctx, id)` | Entity value, or `ErrNotFound` |
| `FindAll(ctx)` | All rows as `[]T` (empty slice for no rows) |
| `FindByQuery(ctx, q)` | Matching rows as `[]T` |
| `FindOneByQuery(ctx, q)` | `*T`, or nil for no row |
| `Count(ctx)` | `int64` row count |
| `Exists(ctx, id)` | Whether the ID exists |
| `Insert`, `Update`, `Delete` | Execute the supplied `*query.Query` and return `sql.Result` |
| `DeleteByID(ctx, id)` | Error, including `ErrNotFound` for an absent ID |
| `DB()` | The supplied `DBTX` handle |
| `TableName()` | The original table name |

`FindByID`, `Exists`, and `DeleteByID` use the column `id`.
The table name is quoted as an identifier. Column mapping uses `db` struct tags.
Use bounded custom queries for large tables.

## Transactions

Use `WithTx` on a write repository built with `db.Writer()`:

```go
err := writeRepo.WithTx(ctx, func(txRepo *repository.Repository[User, string]) error {
    q, err := query.Build(
        "UPDATE users SET email = :email WHERE id = :id",
        map[string]any{"id": "user_123", "email": "new@example.com"},
    )
    if err != nil {
        return err
    }
    _, err = txRepo.Update(ctx, q)
    return err
})
```

A nil callback result commits. An error rolls back. A panic rolls back and propagates.
`WithTx` requires `*sql.DB`
and rejects a repository that already uses `*sql.Tx`.
Use only the supplied repository inside the callback. The outer write
repository needs the writer connection that the callback already holds.

For operations across several repositories, use `db.Write` and build each
repository with the supplied `*sql.Tx`. A read followed by an update then uses
one `BEGIN IMMEDIATE` transaction. See [database](../database/README.md) and
the [consumer guide](../../docs/consumer-guide.md).
