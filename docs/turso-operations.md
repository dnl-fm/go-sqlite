# Turso Operations

The app owns the database. Turso 0.7 makes MVCC and multiprocess WAL mutually
exclusive, so a second process must not open a live `go-sqlite` database from
the side.

`go-sqlite` chooses MVCC and one owner process per physical file. The app's
pooled connections provide same-process concurrency. `EnableMultiprocessWAL`
and `WithMultiprocessWAL` remain only for source compatibility; enabling either
causes `database.Open` to fail.

## Live Reads

Agents and CLI tools read through the owning app's authenticated API or local
admin socket. Prefer named operations that preserve the app's permissions and
business meaning:

```text
appctl customers get customer_01j...
        |
        +-- authenticated local socket or admin API
                    |
                    +-- owning app --> existing MVCC pool --> app.db
```

That command and route are a consumer pattern, not API provided by this
library. Each consuming service documents its real command names, auth method,
pagination, and response shape.

If a service truly needs generic query inspection, keep it admin-only and
read-only. Enforce authentication, query timeouts, row limits, and audit logs.
Do not expose arbitrary SQL writes. Normal agents and CLIs never mutate the
canonical database file directly.

## Offline Forensics

Use one of these artifacts:

1. a snapshot produced by the owning service through its existing database
   connection; or
2. the database after the owner has closed it cleanly.

This library does not implement a snapshot endpoint. A consumer that promises
live exports must implement and test that operation at the service boundary.
Never raw-copy a live Turso database and call the copy evidence.

Inspect the offline artifact with `tursodb`, not system `sqlite3`:

```bash
tursodb snapshot.db "PRAGMA integrity_check;"
tursodb snapshot.db "SELECT COUNT(*) FROM users;"
```

Turso files can be valid and still look broken to system SQLite.

## Supported Shapes

| Shape | Contract |
|---|---|
| one app process, pooled MVCC connections | supported |
| agent or CLI reads through owning app | supported consumer pattern |
| `tursodb` opens owner-produced snapshot or closed DB | supported offline inspection |
| app plus `tursodb --experimental-multiprocess-wal` on live file | rejected by Turso 0.7 MVCC boundary |
| several app processes open the same `go-sqlite` file | unsupported |

If several services need the same data, route operations through one owner or
split the write domain. Do not trade away MVCC silently to make file sharing
look convenient.
