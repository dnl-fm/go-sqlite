package zeit

import "time"

// Duration represents the distance between two Zeit instances.
// Provides multiple unit views of the same span.
// Create via Zeit.Until() or NewDuration().
type Duration struct {
	start *Zeit
	end   *Zeit
}

// NewDuration creates a Duration between two Zeit instances.
// Deprecated: Use start.Until(end) instead.
func NewDuration(start, end *Zeit) *Duration {
	return &Duration{start: start, end: end}
}

// Days returns the total number of calendar days in the duration.
// Truncates partial days (22 hours = 0 days).
func (d *Duration) Days() int {
	return int(d.raw().Hours() / 24)
}

// Hours returns the total number of hours in the duration.
func (d *Duration) Hours() int {
	return int(d.raw().Hours())
}

// Minutes returns the total number of minutes in the duration.
func (d *Duration) Minutes() int {
	return int(d.raw().Minutes())
}

// Seconds returns the total number of seconds in the duration.
func (d *Duration) Seconds() int {
	return int(d.raw().Seconds())
}

// Months returns the number of whole calendar months between start and end.
// Accounts for varying month lengths (28-31 days).
func (d *Duration) Months() int {
	start, end := d.ordered()

	years := end.Year() - start.Year()
	months := int(end.Month()) - int(start.Month())
	total := years*12 + months

	// If the day-of-month hasn't been reached yet, subtract one
	if end.Day() < start.Day() {
		total--
	}

	if total < 0 {
		return 0
	}

	return total
}

// BusinessDays returns the number of business days (Mon-Fri) in the duration.
// Uses [start, end) semantics: start day is counted, end day is not.
func (d *Duration) BusinessDays() int {
	start, end := d.ordered()

	// Normalize to date boundaries
	startDate := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	endDate := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)

	if !startDate.Before(endDate) {
		return 0
	}

	totalDays := int(endDate.Sub(startDate).Hours() / 24)
	fullWeeks := totalDays / 7
	remaining := totalDays % 7

	count := fullWeeks * 5

	for i := range remaining {
		day := startDate.AddDate(0, 0, fullWeeks*7+i).Weekday()
		if day != time.Saturday && day != time.Sunday {
			count++
		}
	}

	return count
}

// Raw returns the underlying time.Duration.
func (d *Duration) Raw() time.Duration {
	return d.raw()
}

// raw returns the absolute duration between start and end.
func (d *Duration) raw() time.Duration {
	diff := d.end.instant.Sub(d.start.instant)
	if diff < 0 {
		return -diff
	}
	return diff
}

// ordered returns start and end as time.Time with start <= end.
func (d *Duration) ordered() (time.Time, time.Time) {
	s := d.start.instant
	e := d.end.instant
	if s.After(e) {
		return e, s
	}
	return s, e
}
