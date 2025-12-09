package zeit

import "time"

// ZeitDuration represents the duration between two Zeit instances
// with methods to calculate in different units including business days.
type ZeitDuration struct {
	start *Zeit
	end   *Zeit
}

// NewDuration creates a ZeitDuration between two Zeit instances.
func NewDuration(start, end *Zeit) *ZeitDuration {
	return &ZeitDuration{
		start: start,
		end:   end,
	}
}

// Days returns the total number of calendar days in the duration.
func (d *ZeitDuration) Days() int {
	duration := d.end.instant.Sub(d.start.instant)
	return int(duration.Hours() / 24)
}

// BusinessDays returns the number of business days (Mon-Fri) in the duration.
// Excludes weekends (Saturday and Sunday).
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
		// Count Monday-Friday (1-5)
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

// Hours returns the total number of hours in the duration.
func (d *ZeitDuration) Hours() int {
	duration := d.end.instant.Sub(d.start.instant)
	return int(duration.Hours())
}

// Minutes returns the total number of minutes in the duration.
func (d *ZeitDuration) Minutes() int {
	duration := d.end.instant.Sub(d.start.instant)
	return int(duration.Minutes())
}

// Seconds returns the total number of seconds in the duration.
func (d *ZeitDuration) Seconds() int {
	duration := d.end.instant.Sub(d.start.instant)
	return int(duration.Seconds())
}

// Raw returns the underlying time.Duration.
func (d *ZeitDuration) Raw() time.Duration {
	return d.end.instant.Sub(d.start.instant)
}
