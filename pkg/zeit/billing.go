package zeit

import "time"

// BillingInterval represents the frequency of billing cycles.
type BillingInterval int

const (
	// Daily billing interval.
	Daily BillingInterval = iota
	// Weekly billing interval.
	Weekly
	// Monthly billing interval.
	Monthly
	// Quarterly billing interval.
	Quarterly
	// Yearly billing interval.
	Yearly
)

// Period represents a time period with start and end times.
type Period struct {
	StartsAt *Zeit
	EndsAt   *Zeit
}

// Cycles generates a series of billing periods starting from the Zeit.
// count: number of periods to generate
// interval: billing frequency (Daily, Weekly, Monthly, etc.)
func (z *Zeit) Cycles(count int, interval BillingInterval) []*Period {
	if count <= 0 {
		return []*Period{}
	}

	periods := make([]*Period, count)
	current := z

	for i := range count {
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

// Duration calculates the time difference between start and end of a period.
func (p *Period) Duration() time.Duration {
	return p.EndsAt.instant.Sub(p.StartsAt.instant)
}

// Contains checks if a Zeit falls within the period.
func (p *Period) Contains(z *Zeit) bool {
	return !z.Before(p.StartsAt) && z.Before(p.EndsAt)
}
