---
tags: [scan, generics, reflection, sql, rows]
status: documented
owner: pkg/scan/
extracted_from: pkg/scan/
---

# Row Scanning Specification

**Type:** Extraction
**Extracted From:** `pkg/scan/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose

Generic row scanning utilities that map SQL query results to Go structs using `db` struct tags. Eliminates manual `rows.Scan()` calls and column order dependencies.

### 1.2 Current Capabilities

- Generic scanning functions for single row, all rows, and optional single row
- Automatic column-to-field mapping via `db` struct tags
- Column order independence (maps by name, not position)
- Partial mapping (unmapped columns silently discarded)
- Struct field caching for performance

### 1.3 Boundaries

- Only works with struct destinations (not maps or primitives)
- Requires `db` tags on fields - no automatic name conversion
- Does not close rows (caller responsibility)
- No nested struct support

---

## 2. Architecture

### 2.1 Component Structure

```
pkg/scan/
├── scan.go           # Core scanning functions and cache
├── scan_test.go      # Unit and integration tests
└── benchmark_test.go # Performance benchmarks
```

### 2.2 Component Diagram

```
┌─────────────────┐
│    Caller       │
│  (repository)   │
└────────┬────────┘
         │ *sql.Rows
         ▼
┌─────────────────┐
│  Row/All/One    │  Generic entry points
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   scanStruct    │  Core scanning logic
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   getFieldMap   │  Cache lookup
└────────┬────────┘
         │ cache miss
         ▼
┌─────────────────┐
│  buildFieldMap  │  Reflection-based mapping
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│   structCache   │  sync.Map storage
└─────────────────┘
```

---

## 3. Core Types

### 3.1 Errors

```go
// Source: pkg/scan/scan.go:12-16
var (
	ErrNotPointer = errors.New("scan: destination must be a pointer")
	ErrNotStruct  = errors.New("scan: destination must be a struct")
)
```

### 3.2 fieldInfo

```go
// Source: pkg/scan/scan.go:18-22
type fieldInfo struct {
	index int    // Struct field index
	name  string // Go field name (not column name)
}
```

### 3.3 structCache

```go
// Source: pkg/scan/scan.go:24-25
var structCache sync.Map // map[reflect.Type]map[string]fieldInfo
```

Global cache storing field mappings per struct type. Uses `sync.Map` for concurrent access without explicit locking.

---

## 4. Data Flow

### 4.1 Generic Entry Points

**Row[T]** - Scan single row (error if no rows)

```go
// Source: pkg/scan/scan.go:27-41
func Row[T any](rows *sql.Rows) (T, error)
```

| Condition | Return |
|-----------|--------|
| Row exists | `(T, nil)` |
| No rows | `(zero T, sql.ErrNoRows)` |
| rows.Err() | `(zero T, err)` |
| Scan error | `(zero T, err)` |

**All[T]** - Scan all rows into slice

```go
// Source: pkg/scan/scan.go:43-59
func All[T any](rows *sql.Rows) ([]T, error)
```

| Condition | Return |
|-----------|--------|
| Rows exist | `([]T, nil)` |
| No rows | `([]T{}, nil)` - empty slice, not nil |
| rows.Err() | `(nil, err)` |
| Scan error | `(nil, err)` |

**One[T]** - Scan optional single row (nil if no rows)

```go
// Source: pkg/scan/scan.go:61-76
func One[T any](rows *sql.Rows) (*T, error)
```

| Condition | Return |
|-----------|--------|
| Row exists | `(*T, nil)` |
| No rows | `(nil, nil)` - NOT an error |
| rows.Err() | `(nil, err)` |
| Scan error | `(nil, err)` |

### 4.2 Scanning Process

```go
// Source: pkg/scan/scan.go:78-106
func scanStruct(rows *sql.Rows, dest any) error
```

1. Validate dest is pointer to struct
2. Get column names from `rows.Columns()`
3. Get/build field map for struct type
4. Create scan destinations array matching column order
5. For each column:
   - If mapped to field: use field address
   - If unmapped: use discard placeholder (`new(any)`)
6. Call `rows.Scan(scanDest...)`

### 4.3 Field Mapping

```go
// Source: pkg/scan/scan.go:108-115
func getFieldMap(t reflect.Type) map[string]fieldInfo
```

Cache lookup with lazy build:
1. Check `structCache` for type
2. If found: return cached map
3. If not: build map, store in cache, return

```go
// Source: pkg/scan/scan.go:117-137
func buildFieldMap(t reflect.Type) map[string]fieldInfo
```

Build process:
1. Iterate struct fields
2. Skip unexported fields
3. Get `db` tag value
4. Skip if tag empty or `"-"`
5. Store `column_name → {index, GoFieldName}`

### 4.4 Wiring Map

| From | To | Trigger |
|------|-----|---------|
| Row/All/One | scanStruct | Each row iteration |
| scanStruct | getFieldMap | Need field mapping |
| getFieldMap | buildFieldMap | Cache miss |
| getFieldMap | structCache | Cache read/write |

---

## 5. Usage Patterns

### 5.1 Struct Definition

```go
type User struct {
	ID    string `db:"id"`
	Email string `db:"email"`
	Name  string `db:"name"`
}
```

Tag format: `db:"column_name"`

### 5.2 Single Row (Required)

```go
rows, _ := db.Query("SELECT * FROM users WHERE id = ?", id)
defer rows.Close()

user, err := scan.Row[User](rows)
if err == sql.ErrNoRows {
	// Handle not found
}
```

### 5.3 Single Row (Optional)

```go
rows, _ := db.Query("SELECT * FROM users WHERE id = ?", id)
defer rows.Close()

user, err := scan.One[User](rows)
if user == nil {
	// Not found, but not an error
}
```

### 5.4 All Rows

```go
rows, _ := db.Query("SELECT * FROM users")
defer rows.Close()

users, err := scan.All[User](rows)
// users is [] (not nil) if no results
```

### 5.5 Partial Mapping

Query more columns than mapped - extra columns discarded:

```go
// Source: pkg/scan/scan_test.go:20-24
type testPartial struct {
	ID   string `db:"id"`
	Name string `db:"name"`
	// Email not mapped - will be ignored
}

rows, _ := db.Query("SELECT * FROM users") // Returns id, email, name
user, _ := scan.Row[testPartial](rows)     // Only maps id, name
```

---

## 6. Performance

### 6.1 Caching Strategy

Struct field mappings cached globally using `sync.Map`:
- First scan of a type: reflection + cache store
- Subsequent scans: cache hit only

### 6.2 Benchmark Results

From `benchmark_test.go`:

| Benchmark | Notes |
|-----------|-------|
| BenchmarkRow | Single row scan |
| BenchmarkAll_100/1000/10000 | Slice scanning at scale |
| BenchmarkOne | Optional row scan |
| BenchmarkManualScan vs BenchmarkAutoScan | Overhead comparison |
| BenchmarkFieldCacheHit | Cache effectiveness |

Key insight: Auto-scan has slight overhead vs manual scan due to reflection-based column matching, but cache eliminates repeated reflection cost.

---

## 9. Validation

### 9.1 Test Coverage

| Test | Purpose |
|------|---------|
| TestRow | Single row success |
| TestRow_NoRows | sql.ErrNoRows handling |
| TestAll | Multiple row scanning |
| TestAll_Empty | Empty slice return |
| TestOne | Optional row success |
| TestOne_NoRows | Nil return without error |
| TestPartialMapping | Unmapped columns ignored |
| TestColumnOrderIndependence | Column order doesn't matter |

### 9.2 Verification Commands

```bash
cd pkg/scan && go test -v
cd pkg/scan && go test -bench=. -benchmem
```

---

## 10. Gaps and Issues

### 10.1 Missing Error Handling

- [ ] No validation that required columns exist (silent zero values)
- [ ] No error on type mismatch during scan (relies on sql.Scan behavior)

### 10.2 Missing Features

- [ ] No nested struct support
- [ ] No pointer field support for nullable columns
- [ ] No JSON tag fallback
- [ ] No custom type converter support

### 10.3 Technical Debt

- Cache never cleared - memory grows with type diversity
- No way to inspect/debug field mappings
- `new(any)` allocation for each unmapped column per scan

### 10.4 Improvement Opportunities

- Add `sql.Null*` type support via wrapper
- Add debug mode to log unmapped columns
- Consider bounded cache with LRU eviction
- Add `Strict` variants that error on unmapped columns
