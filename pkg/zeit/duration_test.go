package zeit

import (
	"testing"
	"time"
)

func TestUntil(t *testing.T) {
	start := Now(time.UTC)
	end := start.Add(24 * time.Hour)

	d := start.Until(end)

	if d == nil {
		t.Fatal("Until() returned nil")
	}
	if d.Days() != 1 {
		t.Errorf("Expected 1 day, got %d", d.Days())
	}
}

func TestDuration_Days(t *testing.T) {
	tests := []struct {
		start    time.Time
		end      time.Time
		name     string
		expected int
	}{
		{
			name:     "Same day",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "One day",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC),
			expected: 1,
		},
		{
			name:     "One week",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC),
			expected: 7,
		},
		{
			name:     "Partial day rounds down",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 16, 8, 0, 0, 0, time.UTC),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.start, time.UTC).Until(New(tt.end, time.UTC))

			result := d.Days()
			if result != tt.expected {
				t.Errorf("Expected %d days, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_Months(t *testing.T) {
	tests := []struct {
		start    time.Time
		end      time.Time
		name     string
		expected int
	}{
		{
			name:     "Same month",
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "One month exact",
			start:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC),
			expected: 1,
		},
		{
			name:     "One month minus one day",
			start:    time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 2, 14, 0, 0, 0, 0, time.UTC),
			expected: 0,
		},
		{
			name:     "Three months",
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC),
			expected: 3,
		},
		{
			name:     "Across year boundary",
			start:    time.Date(2024, 11, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC),
			expected: 3,
		},
		{
			name:     "One year",
			start:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 12,
		},
		{
			name:     "Reversed dates",
			start:    time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
			end:      time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			expected: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.start, time.UTC).Until(New(tt.end, time.UTC))

			result := d.Months()
			if result != tt.expected {
				t.Errorf("Expected %d months, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_BusinessDays(t *testing.T) {
	tests := []struct {
		start    time.Time
		end      time.Time
		name     string
		expected int
	}{
		{
			name:     "Monday to Friday (5 days)",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			end:      time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), // Friday
			expected: 4, // Mon, Tue, Wed, Thu (exclusive end)
		},
		{
			name:     "Monday to Monday (1 week)",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			end:      time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC), // Monday
			expected: 5, // Mon-Fri
		},
		{
			name:     "Friday to Monday (over weekend)",
			start:    time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), // Friday
			end:      time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC), // Monday
			expected: 1, // Just Friday
		},
		{
			name:     "Same day",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			end:      time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC), // Monday
			expected: 0,
		},
		{
			name:     "Saturday to Sunday",
			start:    time.Date(2024, 1, 20, 10, 0, 0, 0, time.UTC), // Saturday
			end:      time.Date(2024, 1, 21, 10, 0, 0, 0, time.UTC), // Sunday
			expected: 0,
		},
		{
			name:     "Two weeks",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			end:      time.Date(2024, 1, 29, 10, 0, 0, 0, time.UTC), // Monday +2 weeks
			expected: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New(tt.start, time.UTC).Until(New(tt.end, time.UTC))

			result := d.BusinessDays()
			if result != tt.expected {
				t.Errorf("Expected %d business days, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_BusinessDays_Reversed(t *testing.T) {
	start := time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC) // Monday
	end := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)   // Monday -1 week

	d := New(start, time.UTC).Until(New(end, time.UTC))

	result := d.BusinessDays()
	if result != 5 {
		t.Errorf("Expected 5 business days for reversed dates, got %d", result)
	}
}

func TestDuration_Hours(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected int
	}{
		{
			name:     "One hour",
			duration: 1 * time.Hour,
			expected: 1,
		},
		{
			name:     "24 hours",
			duration: 24 * time.Hour,
			expected: 24,
		},
		{
			name:     "Partial hour rounds down",
			duration: 90 * time.Minute,
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := Now(time.UTC)
			end := start.Add(tt.duration)

			d := start.Until(end)
			result := d.Hours()

			if result != tt.expected {
				t.Errorf("Expected %d hours, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_Minutes(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected int
	}{
		{
			name:     "One minute",
			duration: 1 * time.Minute,
			expected: 1,
		},
		{
			name:     "One hour",
			duration: 1 * time.Hour,
			expected: 60,
		},
		{
			name:     "90 minutes",
			duration: 90 * time.Minute,
			expected: 90,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := Now(time.UTC)
			end := start.Add(tt.duration)

			d := start.Until(end)
			result := d.Minutes()

			if result != tt.expected {
				t.Errorf("Expected %d minutes, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_Seconds(t *testing.T) {
	start := Now(time.UTC)
	end := start.Add(2*time.Minute + 30*time.Second)

	d := start.Until(end)
	result := d.Seconds()

	expected := 150
	if result != expected {
		t.Errorf("Expected %d seconds, got %d", expected, result)
	}
}

func TestDuration_Raw(t *testing.T) {
	start := Now(time.UTC)
	duration := 5*time.Hour + 30*time.Minute
	end := start.Add(duration)

	d := start.Until(end)
	result := d.Raw()

	if result != duration {
		t.Errorf("Expected raw duration %v, got %v", duration, result)
	}
}

func TestDuration_CrossMonthBoundary(t *testing.T) {
	start := time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 5, 10, 0, 0, 0, time.UTC)

	d := New(start, time.UTC).Until(New(end, time.UTC))

	days := d.Days()
	if days != 5 {
		t.Errorf("Expected 5 days, got %d", days)
	}

	// Wed Jan 31 to Mon Feb 5: Wed, Thu, Fri = 3 business days (Sat/Sun skipped, Mon excluded)
	businessDays := d.BusinessDays()
	if businessDays != 3 {
		t.Errorf("Expected 3 business days, got %d", businessDays)
	}
}

func TestDuration_LeapYear(t *testing.T) {
	start := time.Date(2024, 2, 28, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)

	d := New(start, time.UTC).Until(New(end, time.UTC))

	days := d.Days()
	if days != 2 {
		t.Errorf("Expected 2 days (leap year), got %d", days)
	}
}

func TestDuration_ZeroDuration(t *testing.T) {
	now := Now(time.UTC)
	d := now.Until(now)

	if d.Days() != 0 {
		t.Error("Expected 0 days for same instant")
	}
	if d.BusinessDays() != 0 {
		t.Error("Expected 0 business days for same instant")
	}
	if d.Hours() != 0 {
		t.Error("Expected 0 hours for same instant")
	}
	if d.Minutes() != 0 {
		t.Error("Expected 0 minutes for same instant")
	}
	if d.Months() != 0 {
		t.Error("Expected 0 months for same instant")
	}
}

func TestDuration_DifferentTimezones(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")

	instant := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	start := New(instant, ny)
	end := New(instant.Add(24*time.Hour), tokyo)

	d := start.Until(end)

	if d.Days() != 1 {
		t.Error("Timezone should not affect duration calculation")
	}
}

// Proration scenario test
func TestDuration_Proration(t *testing.T) {
	// Customer subscribes Jan 1, cancels Jan 15
	tz := time.UTC
	billingStart := New(time.Date(2024, 1, 1, 0, 0, 0, 0, tz), tz)
	cancelled := New(time.Date(2024, 1, 15, 0, 0, 0, 0, tz), tz)

	usedDays := billingStart.Until(cancelled).Days()
	totalDays := cancelled.DaysInMonth()

	if usedDays != 14 {
		t.Errorf("Expected 14 used days, got %d", usedDays)
	}
	if totalDays != 31 {
		t.Errorf("Expected 31 total days, got %d", totalDays)
	}

	// Prorate: 14/31 * 100 = ~45.16
	monthlyPrice := 100.0
	proratedPrice := monthlyPrice * float64(usedDays) / float64(totalDays)
	if proratedPrice < 45.0 || proratedPrice > 46.0 {
		t.Errorf("Expected prorated price ~45.16, got %.2f", proratedPrice)
	}
}
