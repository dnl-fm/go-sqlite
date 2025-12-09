# ulid

ULID (Universally Unique Lexicographically Sortable Identifier) generation for Go.

## Overview

ULIDs are 128-bit identifiers that are:
- Lexicographically sortable by timestamp
- Encoded in 26-character Crockford's base32
- Compatible with UUID (same size)
- URL-safe and case-insensitive

## Features

- Timestamp-based sorting
- Monotonic ordering within same millisecond
- Collision-resistant (80 bits of randomness)
- Compact representation (26 characters)

## Installation

```bash
go get github.com/fightbulc/go-turso-kit/pkg/id/ulid
```

## Quick Start

```go
import "github.com/fightbulc/go-turso-kit/pkg/id/ulid"

// Generate ULID
id := ulid.New()
// Example: 01HJ9KXYZ6HDZBQR5CVZW8QTJR
```

## Format

ULID structure:
```
 01HJ9KXYZ6HDZBQR5CVZW8QTJR
 |--------| |--------------|
  Timestamp    Randomness
   (48 bits)    (80 bits)
```

- **Total:** 128 bits (same as UUID)
- **Encoding:** Crockford's base32
- **Length:** 26 characters
- **Character set:** `0123456789ABCDEFGHJKMNPQRSTVWXYZ` (no I, L, O, U)

## API Reference

### New

Generates a new ULID.

```go
func New() string
```

**Example:**
```go
id := ulid.New()
fmt.Println(id)  // 01HJ9KXYZ6HDZBQR5CVZW8QTJR
```

**Characteristics:**
- Timestamp from current time (millisecond precision)
- 80 bits of cryptographic randomness
- Monotonically increasing within same millisecond

## Usage Examples

### Database Primary Keys

```go
type User struct {
    ID        string `db:"id"`
    Email     string `db:"email"`
    CreatedAt string `db:"created_at"`
}

user := User{
    ID:    ulid.New(),
    Email: "alice@example.com",
}
```

### Sortable Event IDs

```go
type Event struct {
    ID        string
    Type      string
    Timestamp time.Time
}

events := []Event{
    {ID: ulid.New(), Type: "login"},
    {ID: ulid.New(), Type: "purchase"},
    {ID: ulid.New(), Type: "logout"},
}

// IDs are already sorted by creation time
sort.Strings(events)  // Chronological order
```

### Request Tracking

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    requestID := ulid.New()
    ctx := context.WithValue(r.Context(), "requestID", requestID)

    log.Printf("[%s] Request started", requestID)
    // ... handle request
    log.Printf("[%s] Request completed", requestID)
}
```

## Properties

### Sortability

ULIDs are lexicographically sortable by creation time:

```go
id1 := ulid.New()
time.Sleep(1 * time.Millisecond)
id2 := ulid.New()
time.Sleep(1 * time.Millisecond)
id3 := ulid.New()

ids := []string{id3, id1, id2}
sort.Strings(ids)
// Result: [id1, id2, id3] - chronological order
```

### Monotonicity

ULIDs generated in the same millisecond are monotonically increasing:

```go
ids := []string{}
for i := 0; i < 1000; i++ {
    ids = append(ids, ulid.New())
}

// Already sorted, even if generated in same millisecond
sort.Strings(ids)  // No change in order
```

### Randomness

Each ULID has 80 bits of randomness:

```go
// Collision probability is extremely low
// 2^80 possible values = ~1.2 × 10^24 combinations
```

## Comparison with Other IDs

### ULID vs UUID

| Feature | ULID | UUID v4 |
|---------|------|---------|
| Size | 128 bits | 128 bits |
| Encoding | Base32 (26 chars) | Hex (36 chars with dashes) |
| Sortable | ✅ Yes | ❌ No |
| Timestamp | ✅ Embedded | ❌ No |
| Random bits | 80 bits | 122 bits |
| URL-safe | ✅ Yes | ⚠️ With dashes |

### ULID vs Auto-Increment

| Feature | ULID | Auto-Increment |
|---------|------|----------------|
| Distributed | ✅ Yes | ❌ No |
| Sortable | ✅ Yes | ✅ Yes |
| Predictable | ❌ No | ✅ Yes |
| Client-side | ✅ Yes | ❌ No |
| DB round-trip | ❌ Not needed | ✅ Required |

### ULID vs NanoID

| Feature | ULID | NanoID |
|---------|------|--------|
| Sortable | ✅ Yes | ❌ No |
| Length | 26 chars | 21 chars (default) |
| Timestamp | ✅ Embedded | ❌ No |
| Customizable | ❌ No | ✅ Yes |

## Best Practices

### 1. Use for Primary Keys

```go
type Entity struct {
    ID string `db:"id"`  // ULID
    // ...
}

entity := Entity{
    ID: ulid.New(),
}
```

### 2. Index Creation

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,  -- ULID stored as TEXT
    email TEXT UNIQUE NOT NULL
);

-- ULIDs are already sorted, no additional index needed for time-based queries
```

### 3. Sorting by Creation Time

```go
// Query users in order of creation
rows, err := db.Query("SELECT * FROM users ORDER BY id")
// ULIDs sort chronologically
```

### 4. Parsing Timestamps

While the package doesn't expose parsing, ULIDs encode timestamps in the first 10 characters:

```go
id := ulid.New()
// First 10 chars encode Unix timestamp in milliseconds (base32)
// Example: 01HJ9KXYZ6... → ~2024-01-15T12:34:56Z
```

## Performance

```
BenchmarkULID/New-8    10000000    120 ns/op    32 B/op    1 allocs/op
```

- **Speed:** ~8 million IDs per second
- **Memory:** 32 bytes per ID
- **Allocations:** 1 allocation per ID

## Use Cases

✅ **Good for:**
- Database primary keys
- Event IDs (sortable by time)
- Distributed systems
- Request/transaction tracking
- Time-series data
- API resource identifiers

❌ **Not ideal for:**
- Cryptographic security (use UUIDv4 or crypto/rand)
- Short URLs (use NanoID)
- User-facing IDs (not human-readable)

## Thread Safety

`ulid.New()` is thread-safe and can be called concurrently:

```go
var wg sync.WaitGroup
for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        id := ulid.New()  // Safe
        // Use id
    }()
}
wg.Wait()
```

## Examples

See [examples/ids/main.go](../../../examples/ids/main.go) for complete working examples.

## See Also

- [nanoid](../nanoid/README.md) - Compact URL-safe IDs
- [ULID Specification](https://github.com/ulid/spec)
- [Examples](../../../examples/ids/)
