# Turso Operations

The app owns the database. Operators inspect it with the same engine, not with
whatever SQLite binary happens to be installed on the box.

`go-sqlite` opens local file databases with Turso MVCC and
`experimental=multiprocess_wal` by default. That means a running app can keep
the file open while `tursodb` reads it from the side:

```bash
tursodb --experimental-multiprocess-wal app.db "select count(*) from users;"
```

Use `tursodb`, not `sqlite3`. Turso files can be valid and still look broken to
system SQLite.

## App Defaults

This is enough for normal apps:

```go
db, err := database.Open(ctx, "app.db")
```

Internally, file-backed local paths are opened like this:

```text
app.db?experimental=multiprocess_wal
```

Existing query parameters are preserved:

```text
app.db?_busy_timeout=5000
app.db?_busy_timeout=5000&experimental=multiprocess_wal
```

In-memory databases and remote URLs are left alone. There is no file for an
operator to inspect in `:memory:`, and remote Turso URLs are not this local
file workflow.

If a test or lab needs to disable live-inspection mode, make that explicit:

```go
cfg := database.DefaultConfig().WithMultiprocessWAL(false)
db, err := database.Open(ctx, "app.db", database.WithConfig(cfg))
```

## What This Does Not Promise

`experimental=multiprocess_wal` is for live inspection. It is not a license to
run a fleet of Go writer processes against the same file.

The lab evidence still says this:

| shape | result |
|---|---|
| one Go app process, pooled MVCC connections | works |
| app process plus matching `tursodb` inspection | works |
| multiple overlapping Go writer processes | still hits WAL file locking |

So the production rule is boring:

one app process owns writes, `tursodb --experimental-multiprocess-wal` inspects
from the side, and support never reaches for `sqlite3` first.
