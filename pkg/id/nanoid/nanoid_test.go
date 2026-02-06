package nanoid

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("generates 21-char ID by default", func(t *testing.T) {
		n := New()
		s := n.String()

		if len(s) != DefaultLength {
			t.Errorf("expected length %d, got %d", DefaultLength, len(s))
		}

		// Verify all characters are valid
		for i, c := range s {
			if !strings.ContainsRune(alphabet, c) {
				t.Errorf("invalid character '%c' at position %d", c, i)
			}
		}
	})

	t.Run("generates unique IDs", func(t *testing.T) {
		seen := make(map[string]bool)
		iterations := 10000

		for range iterations {
			n := New()
			s := n.String()
			if seen[s] {
				t.Errorf("duplicate NanoID generated: %s", s)
			}
			seen[s] = true
		}
	})
}

func TestNewWithLength(t *testing.T) {
	tests := []struct {
		name           string
		length         int
		expectedLength int
	}{
		{
			name:           "custom length 10",
			length:         10,
			expectedLength: 10,
		},
		{
			name:           "custom length 32",
			length:         32,
			expectedLength: 32,
		},
		{
			name:           "default length",
			length:         21,
			expectedLength: 21,
		},
		{
			name:           "minimum length enforced",
			length:         3,
			expectedLength: MinLength, // Should be clamped to 6
		},
		{
			name:           "maximum length enforced",
			length:         1000,
			expectedLength: MaxLength, // Should be clamped to 255
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n := NewWithLength(tt.length)
			s := n.String()

			if len(s) != tt.expectedLength {
				t.Errorf("expected length %d, got %d", tt.expectedLength, len(s))
			}

			// Verify all characters are valid
			for i, c := range s {
				if !strings.ContainsRune(alphabet, c) {
					t.Errorf("invalid character '%c' at position %d", c, i)
				}
			}
		})
	}
}

func TestUniquenessAcrossLengths(t *testing.T) {
	lengths := []int{6, 10, 21, 32, 64}
	iterations := 1000

	for _, length := range lengths {
		t.Run("length="+string(rune(length+'0')), func(t *testing.T) {
			seen := make(map[string]bool)

			for range iterations {
				n := NewWithLength(length)
				s := n.String()
				if seen[s] {
					t.Errorf("duplicate NanoID at length %d: %s", length, s)
				}
				seen[s] = true
			}
		})
	}
}

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid 21-char ID",
			input:   "V1StGXR8_Z5jdHi6B-myT",
			wantErr: false,
		},
		{
			name:    "valid 10-char ID",
			input:   "V1StGXR8_Z",
			wantErr: false,
		},
		{
			name:    "valid with all alphabet chars",
			input:   "_-0123456789abcdefgh",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "too short",
			input:   "abc",
			wantErr: true,
		},
		{
			name:    "invalid character: space",
			input:   "V1StGXR8 Z5jdHi6B-myT",
			wantErr: true,
		},
		{
			name:    "invalid character: special char",
			input:   "V1StGXR8@Z5jdHi6B-myT",
			wantErr: true,
		},
		{
			name:    "invalid character: emoji",
			input:   "V1StGXR8🎉Z5jdHi6B-myT",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := Parse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				// Verify round-trip
				if n.String() != tt.input {
					t.Errorf("round-trip failed: expected %s, got %s", tt.input, n.String())
				}
			}
		})
	}
}

func TestParseRoundTrip(t *testing.T) {
	lengths := []int{6, 10, 21, 32}

	for _, length := range lengths {
		t.Run("length="+string(rune(length+'0')), func(t *testing.T) {
			original := NewWithLength(length)
			originalStr := original.String()

			parsed, err := Parse(originalStr)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			if parsed.String() != originalStr {
				t.Errorf("round-trip failed: expected %s, got %s", originalStr, parsed.String())
			}
		})
	}
}

func TestAlphabetValidation(t *testing.T) {
	t.Run("all alphabet characters are valid", func(t *testing.T) {
		for _, c := range alphabet {
			id := string([]rune{c, c, c, c, c, c}) // MinLength chars
			_, err := Parse(id)
			if err != nil {
				t.Errorf("alphabet character '%c' rejected: %v", c, err)
			}
		}
	})

	t.Run("non-alphabet characters are rejected", func(t *testing.T) {
		invalidChars := []rune{'@', '#', '$', '%', '^', '&', '*', '(', ')', '=', '+', ' ', '\t', '\n'}

		for _, c := range invalidChars {
			id := "V1StGXR8" + string(c) + "Z5jdHi6B-myT"
			_, err := Parse(id)
			if err == nil {
				t.Errorf("invalid character '%c' not rejected", c)
			}
		}
	})
}

func TestString(t *testing.T) {
	t.Run("returns correct string representation", func(t *testing.T) {
		n := New()
		s1 := n.String()
		s2 := n.String()

		if s1 != s2 {
			t.Errorf("String() not consistent: %s != %s", s1, s2)
		}
	})
}

func TestBytes(t *testing.T) {
	t.Run("returns correct byte representation", func(t *testing.T) {
		n := New()
		bytes := n.Bytes()
		str := n.String()

		if string(bytes) != str {
			t.Errorf("Bytes() mismatch: expected %s, got %s", str, string(bytes))
		}
	})
}

func TestJSONMarshaling(t *testing.T) {
	t.Run("marshal and unmarshal", func(t *testing.T) {
		original := New()

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var unmarshaled NanoID
		unmarshalErr := json.Unmarshal(data, &unmarshaled)
		if unmarshalErr != nil {
			t.Fatalf("unmarshal error: %v", unmarshalErr)
		}

		if unmarshaled.String() != original.String() {
			t.Errorf("json round-trip failed: expected %s, got %s",
				original.String(), unmarshaled.String())
		}
	})

	t.Run("marshal custom length", func(t *testing.T) {
		original := NewWithLength(32)

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var unmarshaled NanoID
		unmarshalErr := json.Unmarshal(data, &unmarshaled)
		if unmarshalErr != nil {
			t.Fatalf("unmarshal error: %v", unmarshalErr)
		}

		if len(unmarshaled.String()) != 32 {
			t.Errorf("expected length 32, got %d", len(unmarshaled.String()))
		}

		if unmarshaled.String() != original.String() {
			t.Errorf("json round-trip failed")
		}
	})

	t.Run("unmarshal invalid JSON", func(t *testing.T) {
		invalidJSON := []string{
			`"abc"`,      // too short
			`""`,         // empty
			`123`,        // not a string
			`null`,       // null
			`"invalid@"`, // invalid character
		}

		for _, js := range invalidJSON {
			var n NanoID
			err := json.Unmarshal([]byte(js), &n)
			if err == nil {
				t.Errorf("expected error for JSON: %s", js)
			}
		}
	})
}

func TestInvalidFormatRejection(t *testing.T) {
	invalidInputs := []string{
		"",
		"abc",                       // too short
		"V1StGXR8 Z5jdHi6B-myT",    // space
		"V1StGXR8@Z5jdHi6B-myT",    // invalid char
		"V1StGXR8#Z5jdHi6B-myT",    // invalid char
		strings.Repeat("a", 256),   // too long
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
	for range b.N {
		_ = New()
	}
}

func BenchmarkNewWithLength(b *testing.B) {
	lengths := []int{6, 10, 21, 32, 64}

	for _, length := range lengths {
		b.Run("length="+string(rune(length+'0')), func(b *testing.B) {
			for range b.N {
				_ = NewWithLength(length)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	nanoid := New().String()

	b.ResetTimer()
	for range b.N {
		_, _ = Parse(nanoid)
	}
}

func BenchmarkString(b *testing.B) {
	n := New()

	b.ResetTimer()
	for range b.N {
		_ = n.String()
	}
}

func BenchmarkBytes(b *testing.B) {
	n := New()

	b.ResetTimer()
	for range b.N {
		_ = n.Bytes()
	}
}

func BenchmarkJSONMarshal(b *testing.B) {
	n := New()

	b.ResetTimer()
	for range b.N {
		_, err := json.Marshal(n)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONUnmarshal(b *testing.B) {
	n := New()
	data, err := json.Marshal(n)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for range b.N {
		var result NanoID
		_ = json.Unmarshal(data, &result)
	}
}
