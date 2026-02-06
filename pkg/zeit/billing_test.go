package zeit

import (
	"testing"
	"time"
)

func TestCycles_Daily(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(3, Daily)

	if len(periods) != 3 {
		t.Fatalf("Expected 3 periods, got %d", len(periods))
	}

	// First period: Jan 15 -> Jan 16
	if !periods[0].StartsAt.instant.Equal(start) {
		t.Errorf("Period 0 start incorrect")
	}
	expectedEnd := time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC)
	if !periods[0].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Period 0 end: expected %v, got %v", expectedEnd, periods[0].EndsAt.instant)
	}

	// Second period: Jan 16 -> Jan 17
	if !periods[1].StartsAt.Equal(periods[0].EndsAt) {
		t.Error("Period 1 should start where period 0 ends")
	}

	// Third period: Jan 17 -> Jan 18
	if !periods[2].StartsAt.Equal(periods[1].EndsAt) {
		t.Error("Period 2 should start where period 1 ends")
	}
}

func TestCycles_Weekly(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(2, Weekly)

	if len(periods) != 2 {
		t.Fatalf("Expected 2 periods, got %d", len(periods))
	}

	// First period: Jan 15 -> Jan 22 (7 days)
	expectedEnd := time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC)
	if !periods[0].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Weekly period end: expected %v, got %v", expectedEnd, periods[0].EndsAt.instant)
	}

	// Second period: Jan 22 -> Jan 29 (7 days)
	expectedEnd = time.Date(2024, 1, 29, 10, 0, 0, 0, time.UTC)
	if !periods[1].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Weekly period 2 end: expected %v, got %v", expectedEnd, periods[1].EndsAt.instant)
	}
}

func TestCycles_Monthly(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(3, Monthly)

	if len(periods) != 3 {
		t.Fatalf("Expected 3 periods, got %d", len(periods))
	}

	// First period: Jan 15 -> Feb 15
	expectedEnd := time.Date(2024, 2, 15, 10, 0, 0, 0, time.UTC)
	if !periods[0].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Monthly period 1 end: expected %v, got %v", expectedEnd, periods[0].EndsAt.instant)
	}

	// Second period: Feb 15 -> Mar 15
	expectedEnd = time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)
	if !periods[1].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Monthly period 2 end: expected %v, got %v", expectedEnd, periods[1].EndsAt.instant)
	}

	// Third period: Mar 15 -> Apr 15
	expectedEnd = time.Date(2024, 4, 15, 10, 0, 0, 0, time.UTC)
	if !periods[2].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Monthly period 3 end: expected %v, got %v", expectedEnd, periods[2].EndsAt.instant)
	}
}

func TestCycles_Monthly_EndOfMonth(t *testing.T) {
	// Test month-end edge cases
	start := time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(2, Monthly)

	// Jan 31 + 1 month = Feb 29 (2024 is leap year, Go adjusts to last day)
	// This tests Go's AddDate behavior
	if len(periods) != 2 {
		t.Fatalf("Expected 2 periods, got %d", len(periods))
	}

	// Verify periods are contiguous
	if !periods[1].StartsAt.Equal(periods[0].EndsAt) {
		t.Error("Periods should be contiguous")
	}
}

func TestCycles_Quarterly(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(2, Quarterly)

	if len(periods) != 2 {
		t.Fatalf("Expected 2 periods, got %d", len(periods))
	}

	// First quarter: Jan 15 -> Apr 15
	expectedEnd := time.Date(2024, 4, 15, 10, 0, 0, 0, time.UTC)
	if !periods[0].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Quarterly period 1 end: expected %v, got %v", expectedEnd, periods[0].EndsAt.instant)
	}

	// Second quarter: Apr 15 -> Jul 15
	expectedEnd = time.Date(2024, 7, 15, 10, 0, 0, 0, time.UTC)
	if !periods[1].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Quarterly period 2 end: expected %v, got %v", expectedEnd, periods[1].EndsAt.instant)
	}
}

func TestCycles_Yearly(t *testing.T) {
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	periods := z.Cycles(2, Yearly)

	if len(periods) != 2 {
		t.Fatalf("Expected 2 periods, got %d", len(periods))
	}

	// First year: 2024 Jan 15 -> 2025 Jan 15
	expectedEnd := time.Date(2025, 1, 15, 10, 0, 0, 0, time.UTC)
	if !periods[0].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Yearly period 1 end: expected %v, got %v", expectedEnd, periods[0].EndsAt.instant)
	}

	// Second year: 2025 Jan 15 -> 2026 Jan 15
	expectedEnd = time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	if !periods[1].EndsAt.instant.Equal(expectedEnd) {
		t.Errorf("Yearly period 2 end: expected %v, got %v", expectedEnd, periods[1].EndsAt.instant)
	}
}

func TestCycles_ZeroCount(t *testing.T) {
	z := Now(time.UTC)
	periods := z.Cycles(0, Daily)

	if len(periods) != 0 {
		t.Errorf("Expected 0 periods for count=0, got %d", len(periods))
	}
}

func TestCycles_NegativeCount(t *testing.T) {
	z := Now(time.UTC)
	periods := z.Cycles(-5, Daily)

	if len(periods) != 0 {
		t.Errorf("Expected 0 periods for negative count, got %d", len(periods))
	}
}

func TestCycles_TimezonePreservation(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, ny)

	periods := z.Cycles(2, Daily)

	for i, p := range periods {
		if p.StartsAt.Location() != ny {
			t.Errorf("Period %d StartsAt timezone not preserved", i)
		}
		if p.EndsAt.Location() != ny {
			t.Errorf("Period %d EndsAt timezone not preserved", i)
		}
	}
}

func TestPeriod_Duration(t *testing.T) {
	start := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
	end := New(time.Date(2024, 1, 15, 14, 30, 0, 0, time.UTC), time.UTC)

	period := &Period{
		StartsAt: start,
		EndsAt:   end,
	}

	duration := period.Duration()
	expected := 4*time.Hour + 30*time.Minute

	if duration != expected {
		t.Errorf("Expected duration %v, got %v", expected, duration)
	}
}

func TestPeriod_Contains(t *testing.T) {
	start := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
	end := New(time.Date(2024, 1, 15, 14, 0, 0, 0, time.UTC), time.UTC)

	period := &Period{
		StartsAt: start,
		EndsAt:   end,
	}

	tests := []struct {
		zeit     *Zeit
		name     string
		expected bool
	}{
		{
			name:     "Before period",
			zeit:     New(time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC), time.UTC),
			expected: false,
		},
		{
			name:     "At start (inclusive)",
			zeit:     start,
			expected: true,
		},
		{
			name:     "During period",
			zeit:     New(time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC), time.UTC),
			expected: true,
		},
		{
			name:     "At end (exclusive)",
			zeit:     end,
			expected: false,
		},
		{
			name:     "After period",
			zeit:     New(time.Date(2024, 1, 15, 15, 0, 0, 0, time.UTC), time.UTC),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := period.Contains(tt.zeit)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCycles_Continuity(t *testing.T) {
	// Verify all periods are contiguous (no gaps or overlaps)
	start := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(start, time.UTC)

	intervals := []BillingInterval{Daily, Weekly, Monthly, Quarterly, Yearly}

	for _, interval := range intervals {
		t.Run(interval.String(), func(t *testing.T) {
			periods := z.Cycles(5, interval)

			for i := 1; i < len(periods); i++ {
				if !periods[i].StartsAt.Equal(periods[i-1].EndsAt) {
					t.Errorf("Gap/overlap between period %d and %d", i-1, i)
				}
			}
		})
	}
}

func (bi BillingInterval) String() string {
	switch bi {
	case Daily:
		return "Daily"
	case Weekly:
		return "Weekly"
	case Monthly:
		return "Monthly"
	case Quarterly:
		return "Quarterly"
	case Yearly:
		return "Yearly"
	default:
		return "Unknown"
	}
}
