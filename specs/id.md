---
tags: [id, nanoid, ulid, generation]
status: documented
owner: pkg/id/
extracted_from: pkg/id/
---

# ID Generation Specification

**Type:** Extraction
**Extracted From:** `pkg/id/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose
Provides two ID generation strategies: NanoID for compact URL-safe identifiers, and ULID for time-sortable identifiers with optional prefixes.

### 1.2 Current Capabilities
- NanoID: Cryptographically random, URL-safe, configurable length (6-255 chars)
- ULID: Lexicographically sortable by time, supports entity prefixes (e.g., `user_`)
- Both: Parse/validate, JSON serialization, type-safe wrappers

### 1.3 Boundaries
- No database integration (use with repository layer separately)
- No custom alphabets (fixed per type)
- ULID lowercase not supported (Crockford base32 uppercase only)

---

## 2. Architecture

### 2.1 Component Structure
```
pkg/id/
├── nanoid/
│   ├── nanoid.go       # NanoID type and generation
│   └── nanoid_test.go  # Tests and benchmarks
└── ulid/
    ├── ulid.go         # ULID type and generation
    └── ulid_test.go    # Tests and benchmarks
```

### 2.2 Component Diagram
```
┌──────────────────────────────────────────────────────────────┐
│                        pkg/id                                 │
├─────────────────────────┬────────────────────────────────────┤
│        nanoid/          │              ulid/                  │
├─────────────────────────┼────────────────────────────────────┤
│  NanoID struct          │  ULID struct                       │
│  - value string         │  - value [26]byte                  │
│                         │  - prefix string                   │
├─────────────────────────┼────────────────────────────────────┤
│  New() → NanoID         │  New(prefix) → ULID                │
│  NewWithLength(n)       │  Parse(s) → ULID                   │
│  Parse(s) → NanoID      │  String() → "prefix_ULID"          │
│  String() → string      │  Time() → time.Time (UTC)          │
│  Bytes() → []byte       │  Prefix() → string                 │
│  JSON marshal/unmarshal │  Bytes() → [26]byte                │
│                         │  JSON marshal/unmarshal            │
└─────────────────────────┴────────────────────────────────────┘
```

---

## 3. Core Types

### 3.1 NanoID
```go
// Source: pkg/id/nanoid/nanoid.go:12-15
type NanoID struct {
    value string
}
```

**Alphabet:** `_-0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ` (64 chars)

**Constants:**
```go
// Source: pkg/id/nanoid/nanoid.go:20-26
const (
    DefaultLength = 21  // ~118 bits entropy
    MinLength     = 6
    MaxLength     = 255
)
```

**Errors:**
```go
// Source: pkg/id/nanoid/nanoid.go:28-35
var (
    ErrInvalidFormat    = errors.New("invalid NanoID format")
    ErrInvalidLength    = errors.New("invalid NanoID length: must be between 6 and 255")
    ErrInvalidCharacter = errors.New("invalid character in NanoID")
    ErrEntropyExhausted = errors.New("failed to read random entropy")
)
```

### 3.2 ULID
```go
// Source: pkg/id/ulid/ulid.go:12-16
type ULID struct {
    value  [26]byte
    prefix string
}
```

**Format:** 10 chars timestamp (48-bit) + 16 chars randomness (80-bit)

**Alphabet:** Crockford base32: `0123456789ABCDEFGHJKMNPQRSTVWXYZ` (no I, L, O, U)

**Errors:**
```go
// Source: pkg/id/ulid/ulid.go:21-28
var (
    ErrInvalidFormat    = errors.New("invalid ULID format")
    ErrInvalidLength    = errors.New("invalid ULID length")
    ErrInvalidCharacter = errors.New("invalid character in ULID")
    ErrEntropyExhausted = errors.New("failed to read random entropy")
)
```

---

## 4. Data Flow

### 4.1 NanoID Generation
```go
// Source: pkg/id/nanoid/nanoid.go:38-57
func New() NanoID
func NewWithLength(length int) NanoID
```

1. Clamp length to `[MinLength, MaxLength]`
2. Read `length` random bytes from `crypto/rand`
3. Map each byte to alphabet using `&63` (bitwise mod 64)
4. Return `NanoID{value: string(result)}`

**Fallback:** If `crypto/rand` fails, uses predictable pattern (extremely rare).

### 4.2 NanoID Parsing
```go
// Source: pkg/id/nanoid/nanoid.go:60-76
func Parse(s string) (NanoID, error)
```

1. Reject empty string → `ErrInvalidFormat`
2. Reject length outside `[6, 255]` → `ErrInvalidLength`
3. Validate all chars in alphabet → `ErrInvalidCharacter` if invalid
4. Return `NanoID{value: s}`

### 4.3 ULID Generation
```go
// Source: pkg/id/ulid/ulid.go:31-52
func New(prefix string) ULID
```

1. Create ULID with given prefix
2. Get current time as milliseconds since epoch
3. Encode 48-bit timestamp into first 10 chars (base32)
4. Read 10 random bytes from `crypto/rand`
5. Encode 80 bits into last 16 chars (base32)
6. Return ULID

**Prefix usage:**
```go
u := ulid.New("user_")
fmt.Println(u.String()) // "user_01ARZ3NDEKTSV4RRFFQ69G5FAV"
```

### 4.4 ULID Parsing
```go
// Source: pkg/id/ulid/ulid.go:55-76
func Parse(s string) (ULID, error)
```

1. Reject empty string → `ErrInvalidFormat`
2. Extract prefix if `_` found (e.g., `user_` from `user_01ARZ...`)
3. Validate ULID part is exactly 26 chars → `ErrInvalidLength`
4. Validate all chars are valid base32 → `ErrInvalidCharacter`
5. Return ULID with prefix and value

### 4.5 ULID Time Extraction
```go
// Source: pkg/id/ulid/ulid.go:83-86
func (u ULID) Time() time.Time
```

1. Decode first 10 chars back to 48-bit timestamp
2. Return `time.UnixMilli(timestamp).UTC()`

---

## 5. API Reference

### 5.1 NanoID Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `New` | `func New() NanoID` | Generate 21-char NanoID |
| `NewWithLength` | `func NewWithLength(int) NanoID` | Generate custom length (clamped 6-255) |
| `Parse` | `func Parse(string) (NanoID, error)` | Parse and validate |
| `String` | `func (NanoID) String() string` | Get string value |
| `Bytes` | `func (NanoID) Bytes() []byte` | Get byte slice |
| `MarshalJSON` | `func (NanoID) MarshalJSON() ([]byte, error)` | JSON encode |
| `UnmarshalJSON` | `func (*NanoID) UnmarshalJSON([]byte) error` | JSON decode |

### 5.2 ULID Methods

| Method | Signature | Description |
|--------|-----------|-------------|
| `New` | `func New(prefix string) ULID` | Generate with optional prefix |
| `Parse` | `func Parse(string) (ULID, error)` | Parse and validate |
| `String` | `func (ULID) String() string` | Get full string with prefix |
| `Time` | `func (ULID) Time() time.Time` | Extract timestamp (UTC) |
| `Prefix` | `func (ULID) Prefix() string` | Get prefix |
| `Bytes` | `func (ULID) Bytes() [26]byte` | Get raw 26-byte value |
| `MarshalJSON` | `func (ULID) MarshalJSON() ([]byte, error)` | JSON encode |
| `UnmarshalJSON` | `func (*ULID) UnmarshalJSON([]byte) error` | JSON decode |

---

## 6. Usage Examples

### 6.1 NanoID
```go
import "github.com/fightbulc/go-sqlite/pkg/id/nanoid"

// Generate
id := nanoid.New()                    // 21 chars
shortID := nanoid.NewWithLength(10)   // 10 chars

// Parse
parsed, err := nanoid.Parse("V1StGXR8_Z5jdHi6B-myT")

// Use
fmt.Println(id.String())
data, _ := json.Marshal(id)
```

### 6.2 ULID
```go
import "github.com/fightbulc/go-sqlite/pkg/id/ulid"

// Generate with prefix
userID := ulid.New("user_")           // "user_01ARZ3NDEKTSV4RRFFQ69G5FAV"
orderID := ulid.New("order_")         // "order_01ARZ3NDEKTSV4RRFFQ69G5FAV"
plainID := ulid.New("")               // "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// Parse
parsed, err := ulid.Parse("user_01ARZ3NDEKTSV4RRFFQ69G5FAV")
fmt.Println(parsed.Prefix()) // "user_"
fmt.Println(parsed.Time())   // time.Time in UTC

// Sorting (lexicographic = chronological)
ids := []string{ulid.New("").String()}
time.Sleep(time.Millisecond)
ids = append(ids, ulid.New("").String())
// ids[0] < ids[1] guaranteed
```

---

## 7. Choosing Between NanoID and ULID

| Criteria | NanoID | ULID |
|----------|--------|------|
| Length | 6-255 (default 21) | 26 + prefix |
| Sortable by time | ❌ | ✅ |
| Extract timestamp | ❌ | ✅ |
| Entity prefixes | ❌ | ✅ |
| Shortest possible | 6 chars | 26 chars |
| Use case | Short URLs, tokens | Entity IDs, audit trails |

---

## 8. Test Coverage

### 8.1 NanoID Tests
Source: `pkg/id/nanoid/nanoid_test.go`

| Test | Purpose |
|------|---------|
| `TestNew` | Default length, uniqueness |
| `TestNewWithLength` | Custom lengths, boundary clamping |
| `TestParse` | Valid/invalid inputs |
| `TestParseRoundTrip` | Generate → String → Parse → String |
| `TestAlphabetValidation` | All alphabet chars accepted, others rejected |
| `TestJSONMarshaling` | Marshal/unmarshal round-trip |

### 8.2 ULID Tests
Source: `pkg/id/ulid/ulid_test.go`

| Test | Purpose |
|------|---------|
| `TestNew` | With/without prefix, uniqueness |
| `TestTimestampOrdering` | Lexicographic order = chronological |
| `TestParse` | Valid/invalid, prefix extraction |
| `TestPrefixPreservation` | Various prefixes preserved |
| `TestTimeExtraction` | Timestamp within bounds, UTC |
| `TestJSONMarshaling` | Marshal/unmarshal with prefix |

---

## 9. Validation

### 9.1 Verification Commands
```bash
go test ./pkg/id/... -v
go test ./pkg/id/... -bench=.
```

### 9.2 Benchmarks

**NanoID:**
- `BenchmarkNew` - Generation speed
- `BenchmarkNewWithLength` - Various lengths
- `BenchmarkParse` - Parsing speed
- `BenchmarkJSONMarshal/Unmarshal`

**ULID:**
- `BenchmarkNew` - With/without prefix
- `BenchmarkParse` - Parsing speed
- `BenchmarkTime` - Timestamp extraction
- `BenchmarkJSONMarshal/Unmarshal`

---

## 10. Gaps and Issues

### 10.1 Missing Features
- [ ] Custom alphabets for NanoID
- [ ] Lowercase support for ULID parsing
- [ ] Database scanner/valuer interfaces

### 10.2 Design Notes
- NanoID uses `&63` for alphabet mapping (slight bias, acceptable for use case)
- ULID entropy fallback uses timestamp-based pattern (should never trigger in practice)
- ULID prefix stored separately from value bytes for clean `Bytes()` return

### 10.3 Potential Improvements
- Add `sql.Scanner`/`driver.Valuer` for direct database use
- Add `MustParse()` variants that panic on error
- Add `IsZero()` method for checking uninitialized values
