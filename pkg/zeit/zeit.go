package zeit

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Zeit represents a moment in time with timezone awareness.
// Stores time as UTC internally but preserves user's timezone for display.
type Zeit struct {
	instant  time.Time
	location *time.Location
}

// New creates a Zeit from a time.Time and location.
func New(t time.Time, loc *time.Location) *Zeit {
	if loc == nil {
		loc = time.UTC
	}
	return &Zeit{
		instant:  t.UTC(),
		location: loc,
	}
}

// Now creates a Zeit representing the current moment in the given location.
func Now(loc *time.Location) *Zeit {
	if loc == nil {
		loc = time.UTC
	}
	return New(time.Now(), loc)
}

// FromUser parses an ISO 8601 string and creates a Zeit.
// Expects RFC3339 format: "2006-01-02T15:04:05Z07:00"
func FromUser(isoString string, loc *time.Location) (*Zeit, error) {
	if loc == nil {
		loc = time.UTC
	}

	t, err := time.Parse(time.RFC3339, isoString)
	if err != nil {
		// Try RFC3339Nano for fractional seconds
		t, err = time.Parse(time.RFC3339Nano, isoString)
		if err != nil {
			return nil, err
		}
	}

	return New(t, loc), nil
}

// FromDatabase creates a Zeit from a Unix timestamp (int64).
func FromDatabase(timestamp int64, loc *time.Location) *Zeit {
	if loc == nil {
		loc = time.UTC
	}
	return New(time.Unix(timestamp, 0), loc)
}

// ToDatabase converts Zeit to Unix timestamp for database storage.
func (z *Zeit) ToDatabase() int64 {
	return z.instant.Unix()
}

// ToUser converts Zeit to ISO 8601 format string in the Zeit's timezone.
func (z *Zeit) ToUser() string {
	return z.instant.In(z.location).Format(time.RFC3339)
}

// Add returns a new Zeit with the duration added.
func (z *Zeit) Add(d time.Duration) *Zeit {
	return New(z.instant.Add(d), z.location)
}

// AddDays returns a new Zeit with the specified number of days added.
func (z *Zeit) AddDays(days int) *Zeit {
	return New(z.instant.AddDate(0, 0, days), z.location)
}

// AddBusinessDays returns a new Zeit with business days added (skips weekends).
// Business days are Monday-Friday. Saturday and Sunday are skipped.
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

// Location returns the Zeit's timezone location.
func (z *Zeit) Location() *time.Location {
	return z.location
}

// Time returns the underlying time.Time in the Zeit's timezone.
func (z *Zeit) Time() time.Time {
	return z.instant.In(z.location)
}

// Unix returns the Unix timestamp (seconds since epoch).
func (z *Zeit) Unix() int64 {
	return z.instant.Unix()
}

// Format returns a formatted string representation using the given layout.
// The time is formatted in the Zeit's timezone.
func (z *Zeit) Format(layout string) string {
	return z.instant.In(z.location).Format(layout)
}

// Before reports whether z is before other.
func (z *Zeit) Before(other *Zeit) bool {
	return z.instant.Before(other.instant)
}

// After reports whether z is after other.
func (z *Zeit) After(other *Zeit) bool {
	return z.instant.After(other.instant)
}

// Equal reports whether z and other represent the same instant in time.
func (z *Zeit) Equal(other *Zeit) bool {
	return z.instant.Equal(other.instant)
}

// In returns a new Zeit with the same instant but a different timezone.
// Useful for switching from UTC (database) to user display timezone.
func (z *Zeit) In(loc *time.Location) *Zeit {
	if loc == nil {
		loc = time.UTC
	}
	return &Zeit{
		instant:  z.instant,
		location: loc,
	}
}

// Value implements driver.Valuer for database storage.
// Stores as int64 Unix timestamp (UTC).
func (z *Zeit) Value() (driver.Value, error) {
	return z.instant.Unix(), nil
}

// Scan implements sql.Scanner for database reading.
// Reads int64 Unix timestamp, defaults to UTC. Also normalizes float64
// since some SQLite drivers deliver INTEGER columns as float64.
// Use In() to switch to user timezone after scanning.
//
// Struct fields should use *Zeit (not Zeit) so that driver.Valuer
// is satisfied via the pointer receiver.
func (z *Zeit) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		z.instant = time.Unix(v, 0).UTC()
		z.location = time.UTC
		return nil
	case float64:
		z.instant = time.Unix(int64(v), 0).UTC()
		z.location = time.UTC
		return nil
	case nil:
		return fmt.Errorf("zeit: cannot scan nil value")
	default:
		return fmt.Errorf("zeit: cannot scan %T into Zeit", src)
	}
}

// Until returns a Duration from z to other.
func (z *Zeit) Until(other *Zeit) *Duration {
	return &Duration{start: z, end: other}
}

// DaysInMonth returns the number of days in the Zeit's month (28-31).
func (z *Zeit) DaysInMonth() int {
	t := z.instant.In(z.location)
	// First day of next month, minus one day
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, z.location).Day()
}

// DayOfMonth returns the day of the month (1-31).
func (z *Zeit) DayOfMonth() int {
	return z.instant.In(z.location).Day()
}

// StartOfMonth returns a new Zeit at the first instant of the month (00:00:00 on day 1).
func (z *Zeit) StartOfMonth() *Zeit {
	t := z.instant.In(z.location)
	return New(time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, z.location), z.location)
}

// EndOfMonth returns a new Zeit at the last second of the month (23:59:59 on last day).
func (z *Zeit) EndOfMonth() *Zeit {
	t := z.instant.In(z.location)
	lastDay := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, z.location).Day()
	return New(time.Date(t.Year(), t.Month(), lastDay, 23, 59, 59, 0, z.location), z.location)
}

// MarshalJSON implements json.Marshaler.
func (z *Zeit) MarshalJSON() ([]byte, error) {
	return json.Marshal(z.ToUser())
}

// UnmarshalJSON implements json.Unmarshaler.
func (z *Zeit) UnmarshalJSON(data []byte) error {
	var isoString string
	unmarshalErr := json.Unmarshal(data, &isoString)
	if unmarshalErr != nil {
		return unmarshalErr
	}

	parsed, err := FromUser(isoString, time.UTC)
	if err != nil {
		return err
	}

	z.instant = parsed.instant
	z.location = parsed.location
	return nil
}
