package ulid

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Run("generates valid ULID without prefix", func(t *testing.T) {
		u := New("")
		s := u.String()

		if len(s) != 26 {
			t.Errorf("expected length 26, got %d", len(s))
		}

		// Verify all characters are valid base32
		for i, c := range s {
			if !isValidBase32Char(byte(c)) {
				t.Errorf("invalid character '%c' at position %d", c, i)
			}
		}
	})

	t.Run("generates valid ULID with prefix", func(t *testing.T) {
		u := New("user_")
		s := u.String()

		if !strings.HasPrefix(s, "user_") {
			t.Errorf("expected prefix 'user_', got %s", s)
		}

		if len(s) != 31 { // "user_" (5) + ULID (26)
			t.Errorf("expected length 31, got %d", len(s))
		}
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		seen := make(map[string]bool)
		for range 1000 {
			u := New("")
			s := u.String()
			if seen[s] {
				t.Errorf("duplicate ULID generated: %s", s)
			}
			seen[s] = true
		}
	})
}

func TestTimestampOrdering(t *testing.T) {
	t.Run("newer ULIDs are lexicographically greater", func(t *testing.T) {
		u1 := New("")
		time.Sleep(2 * time.Millisecond) // Ensure different timestamp
		u2 := New("")

		s1 := u1.String()
		s2 := u2.String()

		if s2 <= s1 {
			t.Errorf("expected u2 > u1, got u2=%s, u1=%s", s2, s1)
		}

		// Also check timestamp extraction
		t1 := u1.Time()
		t2 := u2.Time()

		if !t2.After(t1) {
			t.Errorf("expected t2 after t1, got t2=%v, t1=%v", t2, t1)
		}
	})
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid ULID without prefix",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAV",
			wantErr: false,
		},
		{
			name:    "valid ULID with prefix",
			input:   "user_01ARZ3NDEKTSV4RRFFQ69G5FAV",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "too short",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FA",
			wantErr: true,
		},
		{
			name:    "too long",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAVX",
			wantErr: true,
		},
		{
			name:    "invalid character I",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAI",
			wantErr: true,
		},
		{
			name:    "invalid character L",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAL",
			wantErr: true,
		},
		{
			name:    "invalid character O",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAO",
			wantErr: true,
		},
		{
			name:    "invalid character U",
			input:   "01ARZ3NDEKTSV4RRFFQ69G5FAU",
			wantErr: true,
		},
		{
			name:    "lowercase not supported",
			input:   "01arz3ndektsv4rrffq69g5fav",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify round-trip
				if u.String() != tt.input {
					t.Errorf("round-trip failed: expected %s, got %s", tt.input, u.String())
				}
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	t.Run("parse and stringify round-trip without prefix", func(t *testing.T) {
		original := New("")
		originalStr := original.String()

		parsed, err := Parse(originalStr)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if parsed.String() != originalStr {
			t.Errorf("round-trip failed: expected %s, got %s", originalStr, parsed.String())
		}

		// Check timestamp preservation
		if parsed.Time().UnixMilli() != original.Time().UnixMilli() {
			t.Errorf("timestamp mismatch: expected %v, got %v", original.Time(), parsed.Time())
		}
	})

	t.Run("parse and stringify round-trip with prefix", func(t *testing.T) {
		original := New("order_")
		originalStr := original.String()

		parsed, err := Parse(originalStr)
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}

		if parsed.String() != originalStr {
			t.Errorf("round-trip failed: expected %s, got %s", originalStr, parsed.String())
		}

		if parsed.Prefix() != original.Prefix() {
			t.Errorf("prefix mismatch: expected %s, got %s", original.Prefix(), parsed.Prefix())
		}
	})
}

func TestPrefixPreservation(t *testing.T) {
	prefixes := []string{"", "user_", "order_", "payment_", "invoice_"}

	for _, prefix := range prefixes {
		t.Run("prefix="+prefix, func(t *testing.T) {
			u := New(prefix)

			if u.Prefix() != prefix {
				t.Errorf("expected prefix %s, got %s", prefix, u.Prefix())
			}

			s := u.String()
			if prefix != "" && !strings.HasPrefix(s, prefix) {
				t.Errorf("string does not start with prefix: %s", s)
			}
		})
	}
}

func TestTimeExtraction(t *testing.T) {
	t.Run("extracts timestamp in UTC", func(t *testing.T) {
		before := time.Now().UTC().Truncate(time.Millisecond)
		u := New("")
		after := time.Now().UTC().Truncate(time.Millisecond).Add(time.Millisecond)

		extracted := u.Time()

		// Should be in UTC
		if extracted.Location() != time.UTC {
			t.Errorf("expected UTC timezone, got %v", extracted.Location())
		}

		// Should be within the time window (with millisecond precision)
		if extracted.Before(before) || extracted.After(after) {
			t.Errorf("timestamp out of bounds: before=%v, extracted=%v, after=%v",
				before, extracted, after)
		}
	})

	t.Run("handles different timezones", func(t *testing.T) {
		// ULID always stores UTC internally
		u := New("")
		utcTime := u.Time()

		// Convert to different timezone
		loc, err := time.LoadLocation("America/New_York")
		if err != nil {
			t.Skip("timezone data not available")
		}

		nyTime := utcTime.In(loc)

		// Should represent same instant
		if !nyTime.Equal(utcTime) {
			t.Errorf("times not equal: UTC=%v, NY=%v", utcTime, nyTime)
		}

		// Wall clock times should differ (except at midnight UTC or DST transitions)
		_ = nyTime.Hour() // verified via Equal check above
	})
}

func TestJSONMarshaling(t *testing.T) {
	t.Run("marshal and unmarshal without prefix", func(t *testing.T) {
		original := New("")

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var unmarshaled ULID
		unmarshalErr := json.Unmarshal(data, &unmarshaled)
		if unmarshalErr != nil {
			t.Fatalf("unmarshal error: %v", unmarshalErr)
		}

		if unmarshaled.String() != original.String() {
			t.Errorf("json round-trip failed: expected %s, got %s",
				original.String(), unmarshaled.String())
		}
	})

	t.Run("marshal and unmarshal with prefix", func(t *testing.T) {
		original := New("test_")

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var unmarshaled ULID
		unmarshalErr := json.Unmarshal(data, &unmarshaled)
		if unmarshalErr != nil {
			t.Fatalf("unmarshal error: %v", unmarshalErr)
		}

		if unmarshaled.String() != original.String() {
			t.Errorf("json round-trip failed: expected %s, got %s",
				original.String(), unmarshaled.String())
		}

		if unmarshaled.Prefix() != original.Prefix() {
			t.Errorf("prefix mismatch: expected %s, got %s",
				original.Prefix(), unmarshaled.Prefix())
		}
	})

	t.Run("unmarshal invalid JSON", func(t *testing.T) {
		invalidJSON := []string{
			`"invalid"`,
			`"too_short"`,
			`""`,
			`123`,
			`null`,
		}

		for _, js := range invalidJSON {
			var u ULID
			err := json.Unmarshal([]byte(js), &u)
			if err == nil {
				t.Errorf("expected error for JSON: %s", js)
			}
		}
	})
}

func TestBytes(t *testing.T) {
	t.Run("returns 26-byte array", func(t *testing.T) {
		u := New("test_")
		bytes := u.Bytes()

		if len(bytes) != 26 {
			t.Errorf("expected 26 bytes, got %d", len(bytes))
		}

		// Should match the ULID part without prefix
		expected := strings.TrimPrefix(u.String(), u.Prefix())
		actual := string(bytes[:])

		if actual != expected {
			t.Errorf("bytes mismatch: expected %s, got %s", expected, actual)
		}
	})
}

func TestInvalidFormatRejection(t *testing.T) {
	invalidInputs := []string{
		"",
		"invalid",
		"01ARZ3NDEKTSV4RRFFQ69G5FA",     // too short
		"01ARZ3NDEKTSV4RRFFQ69G5FAVXX", // too long
		"01ARZ3NDEKTSV4RRFFQ69G5FAI",   // invalid char I
		"01ARZ3NDEKTSV4RRFFQ69G5FAL",   // invalid char L
		"01ARZ3NDEKTSV4RRFFQ69G5FAO",   // invalid char O
		"01ARZ3NDEKTSV4RRFFQ69G5FAU",   // invalid char U
		"user_invalid",
		"user_",
	}

	for _, input := range invalidInputs {
		t.Run("rejects: "+input, func(t *testing.T) {
			_, err := Parse(input)
			if err == nil {
				t.Errorf("expected error for input: %s", input)
			}
		})
	}
}

// Benchmarks

func BenchmarkNew(b *testing.B) {
	b.Run("without prefix", func(b *testing.B) {
		for range b.N {
			_ = New("")
		}
	})

	b.Run("with prefix", func(b *testing.B) {
		for range b.N {
			_ = New("user_")
		}
	})
}

func BenchmarkParse(b *testing.B) {
	ulid := New("user_").String()

	b.ResetTimer()
	for range b.N {
		_, _ = Parse(ulid)
	}
}

func BenchmarkString(b *testing.B) {
	u := New("user_")

	b.ResetTimer()
	for range b.N {
		_ = u.String()
	}
}

func BenchmarkTime(b *testing.B) {
	u := New("")

	b.ResetTimer()
	for range b.N {
		_ = u.Time()
	}
}

func BenchmarkJSONMarshal(b *testing.B) {
	u := New("user_")

	b.ResetTimer()
	for range b.N {
		_, err := json.Marshal(u)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONUnmarshal(b *testing.B) {
	u := New("user_")
	data, err := json.Marshal(u)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		var result ULID
		_ = json.Unmarshal(data, &result)
	}
}
