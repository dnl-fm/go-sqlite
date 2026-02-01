# Specifications Index

Project: **go-turso-kit** (`github.com/fightbulc/go-turso-kit`)

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

## Package Dependencies

```
┌─────────────┐
│  database   │ ─ Turso/SQLite connection (standalone)
└─────────────┘

┌─────────────┐
│    query    │ ─ Named param → positional (standalone)
└─────────────┘

┌─────────────┐
│    scan     │ ─ Row → struct mapping (standalone)
└─────────────┘

┌─────────────┐     ┌───────┐     ┌──────┐
│ repository  │────▶│ query │     │ scan │
└─────────────┘     └───────┘     └──────┘
       │                              ▲
       └──────────────────────────────┘

┌─────────────┐
│ migrations  │ ─ Schema versioning (standalone)
└─────────────┘

┌─────────────┐
│    zeit     │ ─ Timezone utilities (standalone)
└─────────────┘

┌─────────────┐
│     id      │ ─ NanoID + ULID (standalone)
│  ├─ nanoid  │
│  └─ ulid    │
└─────────────┘
```

**Key relationship:** `repository` uses `query` for param conversion and `scan` for row-to-struct mapping.

## Coverage

All packages documented:
- `pkg/database/` ✓
- `pkg/query/` ✓
- `pkg/scan/` ✓
- `pkg/repository/` ✓
- `pkg/migrations/` ✓
- `pkg/zeit/` ✓
- `pkg/id/nanoid/` ✓
- `pkg/id/ulid/` ✓
- `cmd/migrate/` ✓ (in migrations.md)
- `tmp/examples/` ✓
