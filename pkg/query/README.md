# query

`Build` converts named placeholders into positional SQLite placeholders.
It returns the SQL and its arguments in matching order.

```go
import "github.com/dnl-fm/go-sqlite/pkg/query"

q, err := query.Build(
    "SELECT * FROM users WHERE email = :email AND active = :active",
    map[string]any{"email": "alice@example.com", "active": true},
)
if err != nil {
    return err
}
rows, err := db.Reader().QueryContext(ctx, q.SQL(), q.Args()...)
if err != nil {
    return err
}
defer rows.Close()
```

The resulting SQL is `SELECT * FROM users WHERE email = ? AND active = ?`.
The arguments are `alice@example.com` and `true` in that order.
Repeated placeholders produce repeated arguments. Values remain separate from SQL.

## API

| Function or method | Purpose |
|---|---|
| `Build(sql, params)` | Convert `:name` placeholders with a parameter map |
| `New(sql)` | Create a query without named placeholders |
| `SQL()` | Executable SQL with `?` placeholders |
| `Args()` | Ordered arguments |
| `Original()` | Original SQL |
| `Params()` | Original parameter map |
| `String()` | Executable SQL |
| `ExtractParams(sql)` | Unique parameter names in sorted order |
| `IsValidParamName(name)` | Check the parameter name syntax |

Names start with a letter or underscore. Remaining characters are letters,
digits, or underscores. Empty SQL returns `ErrEmptySQL`. Missing values return
`ErrMissingParam`. Unused values return `ErrExtraParam`.

The placeholder matcher uses a regular expression, not a SQL parser.
Do not put `:name` text inside SQL literals or comments.
Use parameters for values. Parameters cannot replace identifiers such as table names.
See [repository](../repository/README.md) for query execution through repositories.
