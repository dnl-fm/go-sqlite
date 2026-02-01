---
tags: [zeit, time, timezone, billing, business-days, duration]
status: documented
owner: pkg/zeit/
extracted_from: pkg/zeit/
---

# Zeit Specification

**Type:** Extraction
**Extracted From:** `pkg/zeit/`
**Last Updated:** 2026-02-01

---

## 1. Overview

### 1.1 Purpose

Zeit is a timezone-aware time handling package providing:
- UTC internal storage with user timezone preservation
- Database serialization (Unix timestamp)
- User-friendly output (ISO 8601/RFC3339)
- Business day arithmetic (skips weekends)
- Billing cycle generation
- Duration calculations

### 1.2 Current Capabilities

- Create Zeit from time.Time, ISO string, or Unix timestamp
- Add time (duration, days, business days)
- Compare Zeit instances (before, after, equal)
- Generate billing periods (daily, weekly, monthly, quarterly, yearly)
- Calculate durations in various units including business days
- JSON marshaling/unmarshaling
- Timezone preservation across all operations

### 1.3 Boundaries

- Business days exclude only weekends (no holiday support)
- No timezone database updates (uses Go's time.Location)
- No recurring schedule support beyond billing cycles
- UnmarshalJSON always uses UTC location (loses original timezone)

---

## 2. Architecture

### 2.1 Component Structure

```
pkg/zeit/
├── zeit.go          # Core Zeit type, creation, operations
├── zeit_test.go     # Zeit unit tests
├── duration.go      # ZeitDuration type, calculation methods
├── duration_test.go # Duration unit tests
├── billing.go       # BillingInterval, Period, Cycles
├── billing_test.go  # Billing cycle tests
└── README.md        # Package documentation
```

### 2.2 Component Diagram

```
┌─────────────────────────────────────────────────────────────────┐
│                           Zeit                                   │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ instant: time.Time (UTC)                                 │    │
│  │ location: *time.Location (user TZ)                       │    │
│  └─────────────────────────────────────────────────────────┘    │
│                            │                                     │
│            ┌───────────────┼───────────────┐                    │
│            ▼               ▼               ▼                    │
│     ┌───────────┐   ┌───────────┐   ┌────────────┐              │
│     │ ToDatabase│   │  ToUser   │   │   Cycles   │              │
│     │  (int64)  │   │  (RFC3339)│   │  (periods) │              │
│     └───────────┘   └───────────┘   └────────────┘              │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                       ZeitDuration                               │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ start: *Zeit                                             │    │
│  │ end: *Zeit                                               │    │
│  └─────────────────────────────────────────────────────────┘    │
│                            │                                     │
│       ┌────────────────────┼────────────────────┐               │
│       ▼                    ▼                    ▼               │
│  ┌─────────┐        ┌─────────────┐      ┌───────────┐          │
│  │  Days   │        │BusinessDays │      │ Hours/Min │          │
│  └─────────┘        └─────────────┘      └───────────┘          │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                          Period                                  │
│  ┌─────────────────────────────────────────────────────────┐    │
│  │ StartsAt: *Zeit                                          │    │
│  │ EndsAt: *Zeit                                            │    │
│  └─────────────────────────────────────────────────────────┘    │
│                    │                                             │
│       ┌────────────┴────────────┐                               │
│       ▼                         ▼                               │
│  ┌──────────┐            ┌───────────┐                          │
│  │ Duration │            │ Contains  │                          │
│  └──────────┘            └───────────┘                          │
└─────────────────────────────────────────────────────────────────┘
```

---

## 3. Core Types

### 3.1 Zeit

```go
// Source: pkg/zeit/zeit.go:10-16
type Zeit struct {
    instant  time.Time      // Stored as UTC
    location *time.Location // User's timezone
}
```

**Design notes:**
- Immutable: all operations return new Zeit instances
- Internal UTC storage enables correct comparisons across timezones
- Location preserved for display formatting

### 3.2 ZeitDuration

```go
// Source: pkg/zeit/duration.go:5-10
type ZeitDuration struct {
    start *Zeit
    end   *Zeit
}
```

### 3.3 BillingInterval

```go
// Source: pkg/zeit/billing.go:5-13
type BillingInterval int

const (
    Daily BillingInterval = iota
    Weekly
    Monthly
    Quarterly
    Yearly
)
```

### 3.4 Period

```go
// Source: pkg/zeit/billing.go:15-19
type Period struct {
    StartsAt *Zeit
    EndsAt   *Zeit
}
```

---

## 4. Data Flow

### 4.1 Creation Flow

```
User Input (ISO 8601)     time.Time         Database (int64)
       │                      │                    │
       ▼                      ▼                    ▼
   FromUser()              New()            FromDatabase()
       │                      │                    │
       └──────────────────────┼────────────────────┘
                              ▼
                    ┌─────────────────┐
                    │      Zeit       │
                    │ instant (UTC)   │
                    │ location (TZ)   │
                    └─────────────────┘
```

**FromUser parsing:**
1. Try RFC3339 format
2. On failure, try RFC3339Nano (fractional seconds)
3. Return error if both fail

### 4.2 Conversion Flow

```
         Zeit
           │
     ┌─────┼─────┬──────────┐
     ▼     ▼     ▼          ▼
ToDatabase ToUser Time   Format
  (int64) (RFC3339) (time.Time) (custom)
```

### 4.3 Business Day Calculation

```go
// Source: pkg/zeit/zeit.go:67-84
func (z *Zeit) AddBusinessDays(days int) *Zeit {
    current := z.instant
    direction := 1
    if days < 0 {
        direction = -1
        days = -days
    }

    for i := 0; i < days; {
        current = current.AddDate(0, 0, direction)
        weekday := current.Weekday()
        // Skip weekends (Saturday = 6, Sunday = 0)
        if weekday != time.Saturday && weekday != time.Sunday {
            i++
        }
    }

    return New(current, z.location)
}
```

**Behavior:**
- Friday + 1 business day = Monday (skips weekend)
- Monday - 1 business day = Friday (skips weekend)
- Direction handled via sign of days argument

### 4.4 Billing Cycle Generation

```go
// Source: pkg/zeit/billing.go:22-52
func (z *Zeit) Cycles(count int, interval BillingInterval) []*Period {
    if count <= 0 {
        return []*Period{}
    }

    periods := make([]*Period, count)
    current := z

    for i := 0; i < count; i++ {
        var next *Zeit

        switch interval {
        case Daily:
            next = current.AddDays(1)
        case Weekly:
            next = current.AddDays(7)
        case Monthly:
            next = New(current.instant.AddDate(0, 1, 0), current.location)
        case Quarterly:
            next = New(current.instant.AddDate(0, 3, 0), current.location)
        case Yearly:
            next = New(current.instant.AddDate(1, 0, 0), current.location)
        default:
            next = current.AddDays(1)
        }

        periods[i] = &Period{
            StartsAt: current,
            EndsAt:   next,
        }
        current = next
    }

    return periods
}
```

**Properties:**
- Periods are contiguous (no gaps)
- End of period N = Start of period N+1
- Timezone preserved across all periods

### 4.5 Duration BusinessDays

```go
// Source: pkg/zeit/duration.go:22-45
func (d *ZeitDuration) BusinessDays() int {
    start := d.start.instant
    end := d.end.instant

    if start.After(end) {
        start, end = end, start
    }

    count := 0
    current := start

    for !current.After(end) {
        weekday := current.Weekday()
        if weekday != time.Saturday && weekday != time.Sunday {
            count++
        }
        current = current.AddDate(0, 0, 1)
    }

    // Subtract 1 because we're counting inclusive start but exclusive end
    if count > 0 && current.After(end) {
        count--
    }

    return count
}
```

**Note:** Handles reversed dates (end before start) by swapping.

### 4.6 Wiring Map

| From | To | Trigger |
|------|-----|---------|
| zeit.go | time.Time | All operations use Go's time package |
| billing.go | zeit.go | Cycles uses AddDays, New |
| duration.go | zeit.go | Uses instant from Zeit |

---

## 5. API Reference

### 5.1 Creation Functions

| Function | Input | Output | Notes |
|----------|-------|--------|-------|
| `New(t, loc)` | time.Time, *Location | *Zeit | Stores as UTC |
| `Now(loc)` | *Location | *Zeit | Current time |
| `FromUser(s, loc)` | ISO string, *Location | *Zeit, error | RFC3339/Nano |
| `FromDatabase(ts, loc)` | int64, *Location | *Zeit | Unix timestamp |

### 5.2 Zeit Methods

| Method | Returns | Notes |
|--------|---------|-------|
| `ToDatabase()` | int64 | Unix timestamp |
| `ToUser()` | string | RFC3339 in Zeit's timezone |
| `Add(d)` | *Zeit | Add duration |
| `AddDays(n)` | *Zeit | Add calendar days |
| `AddBusinessDays(n)` | *Zeit | Skip weekends |
| `Location()` | *Location | Get timezone |
| `Time()` | time.Time | In Zeit's timezone |
| `Unix()` | int64 | Unix timestamp |
| `Format(layout)` | string | Custom format |
| `Before(other)` | bool | Comparison |
| `After(other)` | bool | Comparison |
| `Equal(other)` | bool | Same instant |
| `MarshalJSON()` | []byte, error | JSON encoding |
| `UnmarshalJSON(data)` | error | JSON decoding |

### 5.3 ZeitDuration Methods

| Method | Returns | Notes |
|--------|---------|-------|
| `Days()` | int | Calendar days |
| `BusinessDays()` | int | Mon-Fri only |
| `Hours()` | int | Total hours |
| `Minutes()` | int | Total minutes |
| `Seconds()` | int | Total seconds |
| `Raw()` | time.Duration | Underlying duration |

### 5.4 Period Methods

| Method | Returns | Notes |
|--------|---------|-------|
| `Duration()` | time.Duration | Period length |
| `Contains(z)` | bool | Start inclusive, end exclusive |

---

## 6. Edge Cases

### 6.1 Nil Location Handling

All creation functions default nil location to UTC:

```go
// Source: pkg/zeit/zeit.go:18-25
func New(t time.Time, loc *time.Location) *Zeit {
    if loc == nil {
        loc = time.UTC
    }
    return &Zeit{
        instant:  t.UTC(),
        location: loc,
    }
}
```

### 6.2 Month Boundary Edge Case

When adding months to end-of-month dates, Go's AddDate handles overflow:
- Jan 31 + 1 month = Feb 28/29 (depending on leap year)

### 6.3 Leap Year Handling

Tests confirm correct handling:
- Feb 28, 2024 + 1 day = Feb 29, 2024 (leap year)
- Feb 29, 2024 + 1 day = Mar 1, 2024

### 6.4 DST Transitions

Handled by Go's time package automatically.

### 6.5 Zero/Negative Cycle Count

```go
periods := z.Cycles(0, Daily)   // []
periods := z.Cycles(-5, Daily)  // []
```

---

## 7. Test Coverage

### 7.1 zeit_test.go

| Test | Coverage |
|------|----------|
| TestNew | Basic creation |
| TestNew_NilLocation | Nil → UTC fallback |
| TestNow | Current time creation |
| TestFromUser | RFC3339 parsing, timezone, nano, errors |
| TestFromDatabase | Unix timestamp loading |
| TestToDatabase | Unix timestamp conversion |
| TestRoundTrip_Database | Bidirectional conversion |
| TestToUser | Timezone-aware formatting |
| TestAdd | Duration arithmetic |
| TestAddDays | Calendar day arithmetic |
| TestAddDays_Negative | Negative days |
| TestAddBusinessDays | Weekend skipping |
| TestLocation | Timezone getter |
| TestTime | Underlying time.Time |
| TestUnix | Unix getter |
| TestFormat | Custom formatting |
| TestBefore/After/Equal | Comparisons |
| TestMarshalJSON/UnmarshalJSON | JSON handling |
| TestJSON_RoundTrip | Bidirectional JSON |
| TestTimezonePreservation | TZ maintained after operations |
| TestDSTTransition | DST handling |
| TestLeapYear | Feb 29 handling |
| TestMonthBoundaries | Month/year rollovers |

### 7.2 duration_test.go

| Test | Coverage |
|------|----------|
| TestNewDuration | Creation |
| TestDuration_Days | Calendar days |
| TestDuration_BusinessDays | Mon-Fri counting |
| TestDuration_BusinessDays_Reversed | Handle end < start |
| TestDuration_Hours/Minutes/Seconds | Time units |
| TestDuration_Raw | Underlying duration |
| TestDuration_CrossMonthBoundary | Month spanning |
| TestDuration_LeapYear | Feb 29 in range |
| TestDuration_ZeroDuration | Same instant |
| TestDuration_DifferentTimezones | TZ independence |

### 7.3 billing_test.go

| Test | Coverage |
|------|----------|
| TestCycles_Daily | 1-day periods |
| TestCycles_Weekly | 7-day periods |
| TestCycles_Monthly | Month periods |
| TestCycles_Monthly_EndOfMonth | Month boundary |
| TestCycles_Quarterly | 3-month periods |
| TestCycles_Yearly | Annual periods |
| TestCycles_ZeroCount | Empty result |
| TestCycles_NegativeCount | Empty result |
| TestCycles_TimezonePreservation | TZ in periods |
| TestPeriod_Duration | Period length |
| TestPeriod_Contains | Containment check |
| TestCycles_Continuity | No gaps/overlaps |

---

## 10. Gaps and Issues

### 10.1 Known Limitations

- [ ] UnmarshalJSON loses timezone (always UTC)
- [ ] No holiday awareness in business days
- [ ] No recurring schedule beyond billing cycles

### 10.2 Potential Improvements

- Add `WithLocation(loc)` to change Zeit timezone without changing instant
- Add holiday calendar support for business day calculations
- Add timezone-aware UnmarshalJSON with location preservation
- Add `BillingInterval.String()` method (currently only in tests)

### 10.3 Documentation Accuracy

Package README is comprehensive and accurate. All documented APIs exist and work as described.
