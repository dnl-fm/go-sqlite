# Zeit - Timezone-Aware Time Package

Zeit is a Go package for timezone-aware time handling with database persistence, billing cycles, and business day calculations.

## Features

- **Timezone Awareness**: Preserves user timezone while storing UTC internally
- **Database Serialization**: Unix timestamp (int64) for efficient storage
- **User-Friendly Format**: ISO 8601 (RFC3339) for API responses
- **Business Day Arithmetic**: Skip weekends (Sat/Sun) in calculations
- **Billing Periods**: Generate cycles (Daily, Weekly, Monthly, Quarterly, Yearly)
- **Duration Calculations**: Days, business days, hours, minutes
- **JSON Marshaling**: Automatic serialization/deserialization
- **Comparison Operations**: Before, After, Equal

## Installation

```go
import "github.com/fightbulc/go-turso-kit/pkg/zeit"
```

## Core Concepts

### Zeit Type

Zeit represents a moment in time with timezone awareness:

```go
type Zeit struct {
    instant  time.Time      // Stored as UTC
    location *time.Location // User's timezone
}
```

### Database vs User Format

- **Database**: Unix timestamp (int64) for efficient storage
- **User**: ISO 8601 string (RFC3339) for readability

```go
z := zeit.Now(time.UTC)
timestamp := z.ToDatabase()  // int64: 1705318200
isoString := z.ToUser()      // string: "2024-01-15T10:30:00Z"
```

## Usage Examples

### Creating Zeit Instances

```go
import (
    "time"
    "github.com/fightbulc/go-turso-kit/pkg/zeit"
)

// Current time
utcNow := zeit.Now(time.UTC)

// From time.Time
t := time.Now()
z := zeit.New(t, time.UTC)

// From user input (ISO 8601)
z, err := zeit.FromUser("2024-01-15T10:30:00Z", time.UTC)

// From database
timestamp := int64(1705318200)
z := zeit.FromDatabase(timestamp, time.UTC)
```

### Timezone Handling

```go
// Create time in different timezones
ny, _ := time.LoadLocation("America/New_York")
tokyo, _ := time.LoadLocation("Asia/Tokyo")

utcTime := zeit.Now(time.UTC)
nyTime := zeit.Now(ny)
tokyoTime := zeit.Now(tokyo)

// Same instant, different representations
fmt.Println(utcTime.ToUser())   // "2024-01-15T15:00:00Z"
fmt.Println(nyTime.ToUser())    // "2024-01-15T10:00:00-05:00"
fmt.Println(tokyoTime.ToUser()) // "2024-01-16T00:00:00+09:00"

// Compare across timezones (compares instants)
utcTime.Equal(nyTime)  // true if same instant
```

### Date Arithmetic

```go
z := zeit.Now(time.UTC)

// Add duration
z.Add(2 * time.Hour)         // +2 hours
z.Add(30 * time.Minute)      // +30 minutes

// Add days
z.AddDays(5)                 // +5 calendar days
z.AddDays(-3)                // -3 calendar days

// Add business days (skip weekends)
friday := zeit.New(time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), time.UTC)
monday := friday.AddBusinessDays(1)  // Skips Sat/Sun -> Monday

// Business day calculation
wed := zeit.New(time.Date(2024, 1, 17, 10, 0, 0, 0, time.UTC), time.UTC)
nextWed := wed.AddBusinessDays(5)    // +5 business days = next Wednesday
```

### Billing Cycles

Generate billing periods with automatic continuity:

```go
start := zeit.New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)

// Daily cycles
periods := start.Cycles(5, zeit.Daily)
// [Jan 1-2, Jan 2-3, Jan 3-4, Jan 4-5, Jan 5-6]

// Weekly cycles
periods := start.Cycles(4, zeit.Weekly)
// [Jan 1-8, Jan 8-15, Jan 15-22, Jan 22-29]

// Monthly cycles
periods := start.Cycles(3, zeit.Monthly)
// [Jan 1 - Feb 1, Feb 1 - Mar 1, Mar 1 - Apr 1]

// Quarterly cycles
periods := start.Cycles(4, zeit.Quarterly)
// [Q1, Q2, Q3, Q4]

// Yearly cycles
periods := start.Cycles(2, zeit.Yearly)
// [2024, 2025]

// Check if time falls within period
period := periods[0]
isInPeriod := period.Contains(someZeit)
```

### Duration Calculations

```go
start := zeit.New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
end := zeit.New(time.Date(2024, 1, 22, 14, 30, 0, 0, time.UTC), time.UTC)

duration := zeit.NewDuration(start, end)

duration.Days()          // 7 (calendar days)
duration.BusinessDays()  // 5 (Mon-Fri, excluding weekends)
duration.Hours()         // 172
duration.Minutes()       // 10350
duration.Seconds()       // 621000
duration.Raw()           // time.Duration
```

### Comparison Operations

```go
z1 := zeit.Now(time.UTC)
time.Sleep(1 * time.Second)
z2 := zeit.Now(time.UTC)

z1.Before(z2)  // true
z1.After(z2)   // false
z1.Equal(z2)   // false

// Works across timezones (compares instants)
utc := zeit.New(someTime, time.UTC)
ny := zeit.New(someTime, nyLocation)
utc.Equal(ny)  // true (same instant)
```

### JSON Serialization

```go
type Event struct {
    Name      string     `json:"name"`
    Timestamp zeit.Zeit  `json:"timestamp"`
}

// Marshal
event := Event{
    Name:      "Meeting",
    Timestamp: *zeit.Now(time.UTC),
}
data, _ := json.Marshal(event)
// {"name":"Meeting","timestamp":"2024-01-15T10:30:00Z"}

// Unmarshal
var restored Event
json.Unmarshal(data, &restored)
```

### Database Integration

```go
// Save to database
z := zeit.Now(time.UTC)
timestamp := z.ToDatabase()  // int64

db.Exec("INSERT INTO events (timestamp) VALUES (?)", timestamp)

// Load from database
var timestamp int64
db.QueryRow("SELECT timestamp FROM events WHERE id = ?", id).Scan(&timestamp)

restored := zeit.FromDatabase(timestamp, time.UTC)
```

## API Reference

### Creation Functions

```go
New(t time.Time, loc *time.Location) *Zeit
Now(loc *time.Location) *Zeit
FromUser(isoString string, loc *time.Location) (*Zeit, error)
FromDatabase(timestamp int64, loc *time.Location) *Zeit
```

### Conversion Methods

```go
ToDatabase() int64              // Unix timestamp
ToUser() string                 // ISO 8601 format
Time() time.Time                // underlying time.Time
Unix() int64                    // Unix timestamp
Format(layout string) string    // Custom format
Location() *time.Location       // Timezone
```

### Arithmetic Methods

```go
Add(d time.Duration) *Zeit
AddDays(days int) *Zeit
AddBusinessDays(days int) *Zeit
```

### Comparison Methods

```go
Before(other *Zeit) bool
After(other *Zeit) bool
Equal(other *Zeit) bool
```

### Billing Methods

```go
Cycles(count int, interval BillingInterval) []*Period
```

### Billing Intervals

```go
const (
    Daily BillingInterval = iota
    Weekly
    Monthly
    Quarterly
    Yearly
)
```

### Period Type

```go
type Period struct {
    StartsAt *Zeit
    EndsAt   *Zeit
}

Duration() time.Duration
Contains(z *Zeit) bool
```

### Duration Type

```go
NewDuration(start, end *Zeit) *ZeitDuration

Days() int
BusinessDays() int
Hours() int
Minutes() int
Seconds() int
Raw() time.Duration
```

## Edge Cases Handled

- **Leap Years**: Feb 29 correctly handled
- **DST Transitions**: Automatic adjustment
- **Month Boundaries**: Jan 31 + 1 month = Feb 28/29
- **Weekend Skipping**: Business days exclude Sat/Sun
- **Timezone Preservation**: Operations maintain original timezone
- **Zero Duration**: Same instant handled correctly

## Testing

Run tests with:

```bash
go test ./pkg/zeit/... -v
```

Coverage:

```bash
go test ./pkg/zeit/... -cover
# coverage: 93.4% of statements
```

## Design Principles

1. **Immutability**: All operations return new Zeit instances
2. **Timezone Safety**: Timezone preserved across operations
3. **Database Efficiency**: Store as int64, display as ISO 8601
4. **Go Standard Library**: Leverage time.Time and time.Location
5. **Business Logic**: Built-in business day calculations
6. **JSON Compatible**: Automatic marshaling/unmarshaling

## Common Patterns

### User Input → Database → User Output

```go
// 1. Parse user input
userInput := "2024-01-15T10:30:00-05:00"  // User in NY timezone
z, _ := zeit.FromUser(userInput, nyLocation)

// 2. Store in database
timestamp := z.ToDatabase()  // Store as int64
db.Exec("INSERT INTO events (ts) VALUES (?)", timestamp)

// 3. Retrieve and display
var ts int64
db.QueryRow("SELECT ts FROM events").Scan(&ts)
restored := zeit.FromDatabase(ts, nyLocation)
output := restored.ToUser()  // "2024-01-15T10:30:00-05:00"
```

### Billing Period Generation

```go
// Generate 12 monthly billing periods
subscriptionStart := zeit.FromUser("2024-01-01T00:00:00Z", time.UTC)
periods := subscriptionStart.Cycles(12, zeit.Monthly)

for i, period := range periods {
    fmt.Printf("Billing Period %d: %s to %s\n",
        i+1,
        period.StartsAt.Format("2006-01-02"),
        period.EndsAt.Format("2006-01-02"))
}
```

### Business Day SLA Calculation

```go
// Ticket created on Friday, 2 business day SLA
ticketCreated := zeit.New(time.Date(2024, 1, 19, 14, 0, 0, 0, time.UTC), time.UTC)
dueDate := ticketCreated.AddBusinessDays(2)  // Tuesday (skips weekend)

fmt.Printf("Due: %s\n", dueDate.Format("Monday, Jan 2, 2006"))
// Output: Due: Tuesday, Jan 22, 2024
```

## License

Part of the go-turso-kit project.
