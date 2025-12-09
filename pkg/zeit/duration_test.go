package zeit

import (
	"testing"
	"time"
)

func TestNewDuration(t *testing.T) {
	start := Now(time.UTC)
	end := start.Add(24 * time.Hour)

	d := NewDuration(start, end)

	if d == nil {
		t.Fatal("NewDuration() returned nil")
	}
	if d.start != start {
		t.Error("Start time mismatch")
	}
	if d.end != end {
		t.Error("End time mismatch")
	}
}

func TestDuration_Days(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
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
			d := NewDuration(
				New(tt.start, time.UTC),
				New(tt.end, time.UTC),
			)

			result := d.Days()
			if result != tt.expected {
				t.Errorf("Expected %d days, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_BusinessDays(t *testing.T) {
	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
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
			expected: 5, // Mon-Fri (5 business days)
		},
		{
			name:     "Friday to Monday (over weekend)",
			start:    time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), // Friday
			end:      time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC), // Monday
			expected: 1, // Just Friday (Mon is exclusive)
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
			expected: 10,                                             // 2 weeks * 5 business days
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDuration(
				New(tt.start, time.UTC),
				New(tt.end, time.UTC),
			)

			result := d.BusinessDays()
			if result != tt.expected {
				t.Errorf("Expected %d business days, got %d", tt.expected, result)
			}
		})
	}
}

func TestDuration_BusinessDays_Reversed(t *testing.T) {
	// Test with end before start (should still calculate correctly)
	start := time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC) // Monday
	end := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)   // Monday -1 week

	d := NewDuration(
		New(start, time.UTC),
		New(end, time.UTC),
	)

	// Should handle reversed dates gracefully
	result := d.BusinessDays()
	if result < 0 {
		t.Error("BusinessDays() should not return negative for reversed dates")
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

			d := NewDuration(start, end)
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

			d := NewDuration(start, end)
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

	d := NewDuration(start, end)
	result := d.Seconds()

	expected := 150 // 2*60 + 30
	if result != expected {
		t.Errorf("Expected %d seconds, got %d", expected, result)
	}
}

func TestDuration_Raw(t *testing.T) {
	start := Now(time.UTC)
	duration := 5*time.Hour + 30*time.Minute
	end := start.Add(duration)

	d := NewDuration(start, end)
	result := d.Raw()

	if result != duration {
		t.Errorf("Expected raw duration %v, got %v", duration, result)
	}
}

func TestDuration_CrossMonthBoundary(t *testing.T) {
	start := time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 5, 10, 0, 0, 0, time.UTC)

	d := NewDuration(
		New(start, time.UTC),
		New(end, time.UTC),
	)

	// 31 Jan -> 5 Feb = 5 days
	days := d.Days()
	if days != 5 {
		t.Errorf("Expected 5 days, got %d", days)
	}

	// Business days: Thu 1 Feb, Fri 2 Feb, Mon 5 Feb (Sat/Sun skipped)
	// Actually: Jan 31 (Wed) is start, Feb 1-2 are Thu-Fri, Feb 3-4 are Sat-Sun, Feb 5 is Mon
	// Counting: Wed, Thu, Fri, Mon = 3 business days (excluding end)
	businessDays := d.BusinessDays()
	if businessDays != 3 {
		t.Errorf("Expected 3 business days, got %d", businessDays)
	}
}

func TestDuration_LeapYear(t *testing.T) {
	// 2024 is a leap year
	start := time.Date(2024, 2, 28, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)

	d := NewDuration(
		New(start, time.UTC),
		New(end, time.UTC),
	)

	// Feb 28 -> Mar 1 = 2 days (includes Feb 29)
	days := d.Days()
	if days != 2 {
		t.Errorf("Expected 2 days (leap year), got %d", days)
	}
}

func TestDuration_ZeroDuration(t *testing.T) {
	now := Now(time.UTC)
	d := NewDuration(now, now)

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
}

func TestDuration_DifferentTimezones(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")

	// Same instant, different timezones
	instant := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	start := New(instant, ny)
	end := New(instant.Add(24*time.Hour), tokyo)

	d := NewDuration(start, end)

	// Should calculate based on actual time difference, not timezone
	if d.Days() != 1 {
		t.Error("Timezone should not affect duration calculation")
	}
}
