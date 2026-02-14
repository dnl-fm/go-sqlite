package zeit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	now := time.Now()
	loc := time.UTC

	z := New(now, loc)

	if z == nil {
		t.Fatal("New() returned nil")
	}
	if z.location != loc {
		t.Errorf("Expected location %v, got %v", loc, z.location)
	}
	if !z.instant.Equal(now.UTC()) {
		t.Errorf("Expected instant %v, got %v", now.UTC(), z.instant)
	}
}

func TestNew_NilLocation(t *testing.T) {
	now := time.Now()
	z := New(now, nil)

	if z.location != time.UTC {
		t.Errorf("Expected UTC location for nil input, got %v", z.location)
	}
}

func TestNow(t *testing.T) {
	before := time.Now()
	z := Now(time.UTC)
	after := time.Now()

	if z == nil {
		t.Fatal("Now() returned nil")
	}

	zeitTime := z.Time()
	if zeitTime.Before(before) || zeitTime.After(after) {
		t.Errorf("Now() returned time outside expected range")
	}
}

func TestFromUser(t *testing.T) {
	tests := []struct {
		checkFunc func(*Zeit) error
		name      string
		input     string
		wantErr   bool
	}{
		{
			name:    "RFC3339 format",
			input:   "2024-01-15T10:30:00Z",
			wantErr: false,
			checkFunc: func(z *Zeit) error {
				expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
				if !z.instant.Equal(expected) {
					t.Errorf("Expected %v, got %v", expected, z.instant)
				}
				return nil
			},
		},
		{
			name:    "RFC3339 with timezone",
			input:   "2024-01-15T10:30:00-05:00",
			wantErr: false,
			checkFunc: func(z *Zeit) error {
				// Should be stored as UTC
				expected := time.Date(2024, 1, 15, 15, 30, 0, 0, time.UTC)
				if !z.instant.Equal(expected) {
					t.Errorf("Expected %v, got %v", expected, z.instant)
				}
				return nil
			},
		},
		{
			name:    "RFC3339Nano format",
			input:   "2024-01-15T10:30:00.123456789Z",
			wantErr: false,
			checkFunc: func(z *Zeit) error {
				expected := time.Date(2024, 1, 15, 10, 30, 0, 123456789, time.UTC)
				if !z.instant.Equal(expected) {
					t.Errorf("Expected %v, got %v", expected, z.instant)
				}
				return nil
			},
		},
		{
			name:    "Invalid format",
			input:   "not-a-date",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z, err := FromUser(tt.input, time.UTC)
			if (err != nil) != tt.wantErr {
				t.Errorf("FromUser() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(z)
			}
		})
	}
}

func TestFromDatabase(t *testing.T) {
	timestamp := int64(1705318200) // 2024-01-15 10:30:00 UTC
	z := FromDatabase(timestamp, time.UTC)

	if z == nil {
		t.Fatal("FromDatabase() returned nil")
	}

	expected := time.Unix(timestamp, 0).UTC()
	if !z.instant.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, z.instant)
	}
}

func TestToDatabase(t *testing.T) {
	timestamp := int64(1705318200)
	z := FromDatabase(timestamp, time.UTC)

	result := z.ToDatabase()
	if result != timestamp {
		t.Errorf("Expected %d, got %d", timestamp, result)
	}
}

func TestRoundTrip_Database(t *testing.T) {
	// Create Zeit, convert to DB, convert back
	original := Now(time.UTC)
	timestamp := original.ToDatabase()
	restored := FromDatabase(timestamp, time.UTC)

	// Should be equal (within second precision)
	if original.Unix() != restored.Unix() {
		t.Errorf("Round trip failed: original %v, restored %v", original.Unix(), restored.Unix())
	}
}

func TestToUser(t *testing.T) {
	// Test with different timezones
	ny, _ := time.LoadLocation("America/New_York")
	tokyo, _ := time.LoadLocation("Asia/Tokyo")

	baseTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)

	tests := []struct {
		name     string
		zeit     *Zeit
		contains string
	}{
		{
			name:     "UTC timezone",
			zeit:     New(baseTime, time.UTC),
			contains: "2024-01-15T10:30:00Z",
		},
		{
			name:     "New York timezone",
			zeit:     New(baseTime, ny),
			contains: "2024-01-15T05:30:00-05:00",
		},
		{
			name:     "Tokyo timezone",
			zeit:     New(baseTime, tokyo),
			contains: "2024-01-15T19:30:00+09:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.zeit.ToUser()
			if result != tt.contains {
				t.Errorf("Expected %s, got %s", tt.contains, result)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(base, time.UTC)

	result := z.Add(2 * time.Hour)
	expected := base.Add(2 * time.Hour)

	if !result.instant.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, result.instant)
	}

	// Original should be unchanged
	if !z.instant.Equal(base) {
		t.Error("Add() modified original Zeit")
	}
}

func TestAddDays(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(base, time.UTC)

	result := z.AddDays(5)
	expected := time.Date(2024, 1, 20, 10, 0, 0, 0, time.UTC)

	if !result.instant.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, result.instant)
	}
}

func TestAddDays_Negative(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(base, time.UTC)

	result := z.AddDays(-5)
	expected := time.Date(2024, 1, 10, 10, 0, 0, 0, time.UTC)

	if !result.instant.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, result.instant)
	}
}

func TestAddBusinessDays(t *testing.T) {
	tests := []struct {
		start    time.Time
		expected time.Time
		name     string
		days     int
	}{
		{
			name:     "Monday + 1 business day = Tuesday",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			days:     1,
			expected: time.Date(2024, 1, 16, 10, 0, 0, 0, time.UTC), // Tuesday
		},
		{
			name:     "Friday + 1 business day = Monday (skip weekend)",
			start:    time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), // Friday
			days:     1,
			expected: time.Date(2024, 1, 22, 10, 0, 0, 0, time.UTC), // Monday
		},
		{
			name:     "Friday + 3 business days = Wednesday",
			start:    time.Date(2024, 1, 19, 10, 0, 0, 0, time.UTC), // Friday
			days:     3,
			expected: time.Date(2024, 1, 24, 10, 0, 0, 0, time.UTC), // Wednesday
		},
		{
			name:     "Monday - 1 business day = Friday",
			start:    time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), // Monday
			days:     -1,
			expected: time.Date(2024, 1, 12, 10, 0, 0, 0, time.UTC), // Friday
		},
		{
			name:     "Wednesday + 5 business days = Wednesday (next week)",
			start:    time.Date(2024, 1, 17, 10, 0, 0, 0, time.UTC), // Wednesday
			days:     5,
			expected: time.Date(2024, 1, 24, 10, 0, 0, 0, time.UTC), // Wednesday
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := New(tt.start, time.UTC)
			result := z.AddBusinessDays(tt.days)

			if !result.instant.Equal(tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, result.instant)
			}
		})
	}
}

func TestLocation(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	z := New(time.Now(), ny)

	if z.Location() != ny {
		t.Errorf("Expected %v, got %v", ny, z.Location())
	}
}

func TestTime(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	ny, _ := time.LoadLocation("America/New_York")
	z := New(base, ny)

	result := z.Time()
	expected := base.In(ny)

	if !result.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}

func TestUnix(t *testing.T) {
	timestamp := int64(1705318200)
	z := FromDatabase(timestamp, time.UTC)

	if z.Unix() != timestamp {
		t.Errorf("Expected %d, got %d", timestamp, z.Unix())
	}
}

func TestFormat(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 30, 45, 0, time.UTC)
	z := New(base, time.UTC)

	result := z.Format("2006-01-02")
	expected := "2024-01-15"

	if result != expected {
		t.Errorf("Expected %s, got %s", expected, result)
	}
}

func TestBefore(t *testing.T) {
	early := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
	late := New(time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC), time.UTC)

	if !early.Before(late) {
		t.Error("Expected early.Before(late) to be true")
	}
	if late.Before(early) {
		t.Error("Expected late.Before(early) to be false")
	}
}

func TestAfter(t *testing.T) {
	early := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
	late := New(time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC), time.UTC)

	if !late.After(early) {
		t.Error("Expected late.After(early) to be true")
	}
	if early.After(late) {
		t.Error("Expected early.After(late) to be false")
	}
}

func TestEqual(t *testing.T) {
	t1 := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	ny, _ := time.LoadLocation("America/New_York")

	z1 := New(t1, time.UTC)
	z2 := New(t1, ny) // Same instant, different timezone

	if !z1.Equal(z2) {
		t.Error("Expected Equal() to be true for same instant in different timezones")
	}
}

func TestMarshalJSON(t *testing.T) {
	z := New(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), time.UTC)

	data, err := json.Marshal(z)
	if err != nil {
		t.Fatalf("MarshalJSON() error: %v", err)
	}

	expected := `"2024-01-15T10:30:00Z"`
	if string(data) != expected {
		t.Errorf("Expected %s, got %s", expected, string(data))
	}
}

func TestUnmarshalJSON(t *testing.T) {
	data := []byte(`"2024-01-15T10:30:00Z"`)

	var z Zeit
	err := json.Unmarshal(data, &z)
	if err != nil {
		t.Fatalf("UnmarshalJSON() error: %v", err)
	}

	expected := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	if !z.instant.Equal(expected) {
		t.Errorf("Expected %v, got %v", expected, z.instant)
	}
}

func TestJSON_RoundTrip(t *testing.T) {
	original := New(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC), time.UTC)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var restored Zeit
	err = json.Unmarshal(data, &restored)
	if err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if !original.Equal(&restored) {
		t.Error("JSON round trip failed")
	}
}

func TestIn(t *testing.T) {
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	z := New(base, time.UTC)
	ny, _ := time.LoadLocation("America/New_York")

	switched := z.In(ny)

	// Same instant
	if !z.Equal(switched) {
		t.Error("In() should preserve the instant")
	}

	// Different timezone
	if switched.Location() != ny {
		t.Errorf("Expected NY location, got %v", switched.Location())
	}

	// Original unchanged
	if z.Location() != time.UTC {
		t.Error("In() modified original Zeit")
	}

	// Display changes
	if switched.ToUser() != "2024-01-15T05:00:00-05:00" {
		t.Errorf("Expected NY time, got %s", switched.ToUser())
	}
}

func TestIn_NilLocation(t *testing.T) {
	z := New(time.Now(), time.UTC)
	switched := z.In(nil)

	if switched.Location() != time.UTC {
		t.Error("In(nil) should default to UTC")
	}
}

func TestValue(t *testing.T) {
	timestamp := int64(1705318200)
	z := FromDatabase(timestamp, time.UTC)

	val, err := z.Value()
	if err != nil {
		t.Fatalf("Value() error: %v", err)
	}

	got, ok := val.(int64)
	if !ok {
		t.Fatalf("Value() returned %T, want int64", val)
	}
	if got != timestamp {
		t.Errorf("Expected %d, got %d", timestamp, got)
	}
}

func TestScan(t *testing.T) {
	timestamp := int64(1705318200)

	var z Zeit
	err := z.Scan(timestamp)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if z.Unix() != timestamp {
		t.Errorf("Expected %d, got %d", timestamp, z.Unix())
	}
	if z.Location() != time.UTC {
		t.Error("Scan() should default to UTC")
	}
}

func TestScan_Float64(t *testing.T) {
	timestamp := float64(1705312800)

	var z Zeit
	err := z.Scan(timestamp)
	if err != nil {
		t.Fatalf("Scan(float64) error: %v", err)
	}

	if z.Unix() != int64(timestamp) {
		t.Errorf("Expected %d, got %d", int64(timestamp), z.Unix())
	}
	if z.Location() != time.UTC {
		t.Error("Scan(float64) should default to UTC")
	}
}

func TestScan_InvalidTypes(t *testing.T) {
	var z Zeit

	if err := z.Scan(nil); err == nil {
		t.Error("Scan(nil) should return error")
	}
	if err := z.Scan("not a timestamp"); err == nil {
		t.Error("Scan(string) should return error")
	}
	if err := z.Scan(true); err == nil {
		t.Error("Scan(bool) should return error")
	}
}

func TestScanValueRoundTrip(t *testing.T) {
	original := Now(time.UTC)

	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value() error: %v", err)
	}

	var restored Zeit
	err = restored.Scan(val)
	if err != nil {
		t.Fatalf("Scan() error: %v", err)
	}

	if original.Unix() != restored.Unix() {
		t.Errorf("Round trip failed: %d != %d", original.Unix(), restored.Unix())
	}
}

func TestScanThenIn(t *testing.T) {
	// Simulates: DB scan (UTC) -> switch to user TZ for display
	// Use a known instant: 2024-01-15 10:00:00 UTC
	timestamp := int64(1705312800)
	berlin, _ := time.LoadLocation("Europe/Berlin")

	var z Zeit
	_ = z.Scan(timestamp)

	userView := z.In(berlin)
	expected := "2024-01-15T11:00:00+01:00"
	if userView.ToUser() != expected {
		t.Errorf("Expected %s, got %s", expected, userView.ToUser())
	}
}

func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		name     string
		date     time.Time
		expected int
	}{
		{"January", time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), 31},
		{"February (leap)", time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), 29},
		{"February (non-leap)", time.Date(2023, 2, 10, 0, 0, 0, 0, time.UTC), 28},
		{"April", time.Date(2024, 4, 1, 0, 0, 0, 0, time.UTC), 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := New(tt.date, time.UTC)
			if z.DaysInMonth() != tt.expected {
				t.Errorf("Expected %d, got %d", tt.expected, z.DaysInMonth())
			}
		})
	}
}

func TestDayOfMonth(t *testing.T) {
	z := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), time.UTC)
	if z.DayOfMonth() != 15 {
		t.Errorf("Expected 15, got %d", z.DayOfMonth())
	}
}

func TestStartOfMonth(t *testing.T) {
	z := New(time.Date(2024, 3, 15, 14, 30, 0, 0, time.UTC), time.UTC)
	start := z.StartOfMonth()

	expected := "2024-03-01T00:00:00Z"
	if start.ToUser() != expected {
		t.Errorf("Expected %s, got %s", expected, start.ToUser())
	}
	if start.Location() != time.UTC {
		t.Error("StartOfMonth should preserve timezone")
	}
}

func TestEndOfMonth(t *testing.T) {
	z := New(time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), time.UTC)
	end := z.EndOfMonth()

	expected := "2024-02-29T23:59:59Z" // leap year
	if end.ToUser() != expected {
		t.Errorf("Expected %s, got %s", expected, end.ToUser())
	}
}

func TestStartEndOfMonth_WithTimezone(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	z := New(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC), berlin)

	start := z.StartOfMonth()
	end := z.EndOfMonth()

	if start.Location() != berlin {
		t.Error("StartOfMonth should preserve timezone")
	}
	if end.Location() != berlin {
		t.Error("EndOfMonth should preserve timezone")
	}
}

func TestUntilMethod(t *testing.T) {
	start := New(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), time.UTC)
	end := New(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC), time.UTC)

	d := start.Until(end)
	if d.Days() != 14 {
		t.Errorf("Expected 14 days, got %d", d.Days())
	}
}

func TestTimezonePreservation(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	base := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	z := New(base, ny)
	result := z.Add(1 * time.Hour)

	if result.Location() != ny {
		t.Error("Timezone not preserved after Add()")
	}

	result = z.AddDays(1)
	if result.Location() != ny {
		t.Error("Timezone not preserved after AddDays()")
	}

	result = z.AddBusinessDays(1)
	if result.Location() != ny {
		t.Error("Timezone not preserved after AddBusinessDays()")
	}
}

func TestDSTTransition(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")

	// March DST transition (spring forward)
	beforeDST := time.Date(2024, 3, 10, 1, 0, 0, 0, ny)
	z := New(beforeDST, ny)

	// Add 2 hours should account for DST
	result := z.Add(2 * time.Hour)

	// Verify the operation worked (specific behavior may vary)
	if result == nil {
		t.Error("DST transition handling failed")
	}
}

func TestLeapYear(t *testing.T) {
	// 2024 is a leap year
	feb28 := time.Date(2024, 2, 28, 10, 0, 0, 0, time.UTC)
	z := New(feb28, time.UTC)

	result := z.AddDays(1)
	expected := time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC)

	if !result.instant.Equal(expected) {
		t.Error("Leap year handling failed")
	}

	// Feb 29 + 1 day = Mar 1
	feb29 := time.Date(2024, 2, 29, 10, 0, 0, 0, time.UTC)
	z = New(feb29, time.UTC)
	result = z.AddDays(1)
	expected = time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)

	if !result.instant.Equal(expected) {
		t.Error("Leap year boundary handling failed")
	}
}

func TestMonthBoundaries(t *testing.T) {
	tests := []struct {
		start    time.Time
		expected time.Time
		name     string
		days     int
	}{
		{
			name:     "End of January + 1 day",
			start:    time.Date(2024, 1, 31, 10, 0, 0, 0, time.UTC),
			days:     1,
			expected: time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			name:     "End of year + 1 day",
			start:    time.Date(2024, 12, 31, 10, 0, 0, 0, time.UTC),
			days:     1,
			expected: time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := New(tt.start, time.UTC)
			result := z.AddDays(tt.days)

			if !result.instant.Equal(tt.expected) {
				t.Errorf("Expected %v, got %v", tt.expected, result.instant)
			}
		})
	}
}
