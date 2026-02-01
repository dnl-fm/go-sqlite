# Specifications Index

Project: **go-sqlite**

## Active

| Spec | Code | Status | Purpose |
|------|------|--------|---------|
| [database.md](./database.md) | `pkg/database/` | Documented | Database wrapper, connection pool, pragmas, transactions |
| [query.md](./query.md) | `pkg/query/` | Documented | Named parameter queries, :name to ? conversion |
| [scan.md](./scan.md) | `pkg/scan/` | Documented | Generic row scanning, db tags, struct caching |
| [repository.md](./repository.md) | `pkg/repository/` | Documented | Generic CRUD, transactions, query-based operations |
| [migrations.md](./migrations.md) | `pkg/migrations/`, `cmd/migrate/` | Documented | Schema migrations, version tracking, CLI tool |
| [id.md](./id.md) | `pkg/id/` | Documented | NanoID and ULID generation, URL-safe and time-sortable IDs |
| [zeit.md](./zeit.md) | `pkg/zeit/` | Documented | Timezone-aware time, business days, billing cycles, duration |
| [examples.md](./examples.md) | `tmp/examples/` | Documented | Working examples: repository, transactions, migrations, zeit |
