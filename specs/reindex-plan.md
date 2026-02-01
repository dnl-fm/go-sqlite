# go-sqlite Reindex Plan

**Type:** extraction
**Source:** entire codebase
**Created:** 2026-02-01

---

## Context

SQLite/Turso toolkit providing database abstractions, migrations, generic repository pattern, query building, row scanning, ID generation, and timezone utilities. Well-documented with READMEs per package. Core dependency: `turso.tech/database/tursogo`.

## Phase 1: Core Database Layer ✅ COMPLETED

- [x] Extract pkg/database/config.go → Config struct, driver types (sqlite3, turso)
- [x] Extract pkg/database/database.go → Database wrapper, connection handling, Exec/Query/QueryRow methods
- [x] Extract pkg/database/errors.go → ErrInvalidConfig, ErrConnection
- [x] Extract pkg/database/database_test.go → Test patterns
      Output: specs/database.md

## Phase 2: Query Building ✅ COMPLETED

- [x] Extract pkg/query/query.go → Query type, Build, New (note: README incorrectly documents BuildDirect/BuildConverted)
- [x] Extract pkg/query/params.go → ExtractParams, IsValidParamName (note: UnusedParams doesn't exist)
- [x] Extract pkg/query/query_test.go → Unit tests
- [x] Extract pkg/query/integration_test.go → Integration tests
- [x] Extract pkg/query/benchmark_test.go → Performance benchmarks
      Output: specs/query.md

## Phase 3: Row Scanning ✅ COMPLETED

- [x] Extract pkg/scan/scan.go → Row[T], All[T], One[T] generic functions, structCache, fieldInfo
- [x] Extract pkg/scan/scan_test.go → Test patterns
- [x] Extract pkg/scan/benchmark_test.go → Performance benchmarks
      Output: specs/scan.md

## Phase 4: Repository Pattern ✅ COMPLETED

- [x] Extract pkg/repository/repository.go → Repository[T, ID], CRUD operations (no Entity interface)
- [x] Extract pkg/repository/transaction.go → TxRepository, WithTx, automatic rollback
- [x] Extract pkg/repository/repository_test.go → Unit tests
- [x] Extract pkg/repository/transaction_test.go → Transaction tests
- [x] Extract pkg/repository/benchmark_test.go → Performance benchmarks
      Output: specs/repository.md

## Phase 5: Migration System ✅ COMPLETED

- [x] Extract pkg/migrations/migrations.go → Migration type, Register, Run, Rollback, Status
- [x] Extract pkg/migrations/runner.go → Runner implementation
- [x] Extract pkg/migrations/migrations_test.go → Test patterns
- [x] Extract cmd/migrate/main.go → CLI entry point, printUsage
- [x] Extract cmd/migrate/commands.go → runUp, runDown, runStatus, runCreate
- [x] Extract migrations/20251107005645_example_users_table.go → Example migration pattern
      Output: specs/migrations.md

## Phase 6: ID Generation

- [ ] Extract pkg/id/nanoid/nanoid.go → New, NewWithConfig
- [ ] Extract pkg/id/nanoid/nanoid_test.go → Tests
- [ ] Extract pkg/id/ulid/ulid.go → New, ULID format
- [ ] Extract pkg/id/ulid/ulid_test.go → Tests
      Output: specs/id.md

## Phase 7: Zeit (Time Utilities)

- [ ] Extract pkg/zeit/zeit.go → Zeit type, Now, New, FromUser, FromDatabase, ToDatabase, ToUser
- [ ] Extract pkg/zeit/duration.go → ZeitDuration, Days, BusinessDays
- [ ] Extract pkg/zeit/billing.go → BillingInterval, Cycles, Period
- [ ] Extract pkg/zeit/zeit_test.go → Core tests
- [ ] Extract pkg/zeit/duration_test.go → Duration tests
- [ ] Extract pkg/zeit/billing_test.go → Billing cycle tests
      Output: specs/zeit.md

## Phase 8: Examples

- [ ] Extract tmp/examples/migrations/main.go → Programmatic migration usage
- [ ] Extract tmp/examples/repository/main.go → Repository CRUD example
- [ ] Extract tmp/examples/timezones/main.go → Zeit usage example
- [ ] Extract tmp/examples/transactions/main.go → Transaction handling
      Output: specs/examples.md

## Phase Final: Synthesis

- [ ] Create specs/readme.md (PIN) indexing all specs
- [ ] Verify all components documented
- [ ] Cross-reference package dependencies (database → query → repository → scan)