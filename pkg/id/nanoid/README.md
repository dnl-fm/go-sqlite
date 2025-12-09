# nanoid

Compact, URL-safe, unique string ID generator for Go.

## Overview

NanoID is a tiny, secure, URL-friendly unique string ID generator with:
- Customizable alphabet and length
- URL-safe default alphabet
- Cryptographically strong random generation
- Smaller than UUID (21 vs 36 characters)

## Features

- Compact IDs (21 characters default)
- URL-safe alphabet
- Collision-resistant
- Customizable alphabet and length
- No special characters (by default)

## Installation

```bash
go get github.com/fightbulc/go-turso-kit/pkg/id/nanoid
```

## Quick Start

```go
import "github.com/fightbulc/go-turso-kit/pkg/id/nanoid"

// Generate NanoID with default settings
id := nanoid.New()
// Example: V1StGXR8_Z5jdHi6B-myT
```

## Default Configuration

- **Length:** 21 characters
- **Alphabet:** `0-9A-Za-z_-` (64 characters)
- **Collision probability:** ~1% after generating 1 billion IDs

## API Reference

### New

Generates a new NanoID with default settings.

```go
func New() string
```

**Example:**
```go
id := nanoid.New()
fmt.Println(id)  // V1StGXR8_Z5jdHi6B-myT
```

### NewWithConfig

Generates a new NanoID with custom configuration.

```go
func NewWithConfig(alphabet string, length int) string
```

**Parameters:**
- `alphabet` - Character set to use
- `length` - Length of generated ID

**Example:**
```go
// Numbers only, 10 characters
id := nanoid.NewWithConfig("0123456789", 10)
// Example: 4930291839

// Lowercase letters only, 15 characters
id := nanoid.NewWithConfig("abcdefghijklmnopqrstuvwxyz", 15)
// Example: xkwmnjqopzrsvbh

// Custom alphabet with symbols
id := nanoid.NewWithConfig("ABCDEF123456", 8)
// Example: 4B3F21AC
```

## Usage Examples

### Database Primary Keys

```go
type Product struct {
    ID    string `db:"id"`
    Name  string `db:"name"`
    Price int    `db:"price"`
}

product := Product{
    ID:    nanoid.New(),
    Name:  "Widget",
    Price: 1999,
}
```

### Short URLs

```go
type ShortURL struct {
    ID       string  // Short code
    LongURL  string
}

shortURL := ShortURL{
    ID:      nanoid.NewWithConfig("0123456789abcdefghijklmnopqrstuvwxyz", 8),
    LongURL: "https://example.com/very/long/url",
}

// Access: https://short.link/a4b3c2d1
```

### Session IDs

```go
func createSession(userID string) string {
    sessionID := nanoid.New()

    // Store session
    sessions[sessionID] = Session{
        ID:     sessionID,
        UserID: userID,
        Expires: time.Now().Add(24 * time.Hour),
    }

    return sessionID
}
```

### API Keys

```go
func generateAPIKey() string {
    // Longer, more secure for API keys
    return nanoid.NewWithConfig(
        "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz",
        32,
    )
}
// Example: kH3fB7x9wQ2mL5pY8tR1vN4cZ6gJ0sA9
```

### File Upload IDs

```go
func handleUpload(file multipart.File) string {
    // Short, readable filename
    id := nanoid.NewWithConfig("abcdefghijklmnopqrstuvwxyz", 12)
    ext := filepath.Ext(file.Filename)

    filename := id + ext
    // Example: xkwmnjqopzrs.jpg

    return filename
}
```

## Custom Alphabets

### Numbers Only

```go
id := nanoid.NewWithConfig("0123456789", 10)
// Example: 4930291839
```

### Lowercase Letters Only

```go
id := nanoid.NewWithConfig("abcdefghijklmnopqrstuvwxyz", 15)
// Example: xkwmnjqopzrsvbh
```

### No Ambiguous Characters

```go
// Remove similar looking characters (0, O, I, l, 1)
alphabet := "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz"
id := nanoid.NewWithConfig(alphabet, 21)
// Example: BhN2xK8Pm9Rs5TvWz3Yq4
```

### Hex Only

```go
id := nanoid.NewWithConfig("0123456789abcdef", 16)
// Example: 4b3f21ac8d6e9f05
```

## Collision Probability

Default settings (21 characters, 64-character alphabet):

| IDs Generated | Collision Probability |
|---------------|----------------------|
| 1,000 | ~0.00000001% |
| 1,000,000 | ~0.0001% |
| 1,000,000,000 | ~1% |

Custom length collision calculator:
- **10 chars:** Safe for ~10,000 IDs
- **15 chars:** Safe for ~10 million IDs
- **21 chars:** Safe for ~1 billion IDs
- **32 chars:** Safe for practically unlimited IDs

## Comparison with Other IDs

### NanoID vs ULID

| Feature | NanoID | ULID |
|---------|--------|------|
| Length | 21 chars (default) | 26 chars |
| Sortable | ❌ No | ✅ Yes |
| Timestamp | ❌ No | ✅ Yes |
| Customizable | ✅ Yes | ❌ No |
| URL-safe | ✅ Yes | ✅ Yes |

### NanoID vs UUID

| Feature | NanoID | UUID v4 |
|---------|--------|---------|
| Length | 21 chars | 36 chars |
| URL-safe | ✅ Yes (default) | ⚠️ With dashes |
| Customizable | ✅ Yes | ❌ No |
| Standard | Custom | RFC 4122 |

## Best Practices

### 1. Choose Length Based on Volume

```go
// Low volume (~1000 IDs)
id := nanoid.NewWithConfig(alphabet, 10)

// Medium volume (~1 million IDs)
id := nanoid.NewWithConfig(alphabet, 15)

// High volume (~1 billion IDs)
id := nanoid.New()  // Default 21
```

### 2. Use Longer IDs for Security-Critical Uses

```go
// API keys, tokens
apiKey := nanoid.NewWithConfig(alphabet, 32)

// Session IDs
sessionID := nanoid.NewWithConfig(alphabet, 24)
```

### 3. Remove Ambiguous Characters for User-Facing IDs

```go
// No 0/O, 1/I/l confusion
alphabet := "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
code := nanoid.NewWithConfig(alphabet, 8)
// User can easily type: BH3NK8PM
```

### 4. Database Storage

```sql
CREATE TABLE products (
    id TEXT PRIMARY KEY,  -- NanoID stored as TEXT
    name TEXT NOT NULL
);
```

## Performance

```
BenchmarkNanoID/New-8           5000000    240 ns/op    64 B/op    2 allocs/op
BenchmarkNanoID/Custom-8        3000000    450 ns/op    96 B/op    3 allocs/op
```

- **Speed:** ~4 million IDs per second (default)
- **Memory:** 64 bytes per ID (default)
- **Custom:** Slightly slower due to alphabet processing

## Use Cases

✅ **Good for:**
- Short URLs
- File upload names
- User-facing codes
- Compact database keys
- API resource identifiers
- Invite codes
- Promo codes

❌ **Not ideal for:**
- Time-sorted data (use ULID)
- Chronological ordering (use ULID)
- High-security tokens (use longer length)

## Thread Safety

`nanoid.New()` and `nanoid.NewWithConfig()` are thread-safe:

```go
var wg sync.WaitGroup
for i := 0; i < 1000; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        id := nanoid.New()  // Safe
        // Use id
    }()
}
wg.Wait()
```

## Examples

See [examples/ids/main.go](../../../examples/ids/main.go) for complete working examples.

## Security

- Uses `crypto/rand` for cryptographically secure randomness
- Default alphabet avoids URL encoding issues
- Configurable length for desired collision resistance

## See Also

- [ulid](../ulid/README.md) - Sortable time-based IDs
- [NanoID Project](https://github.com/ai/nanoid)
- [Examples](../../../examples/ids/)
