// Package ulid provides ULID (Universally Unique Lexicographically Sortable Identifier) generation.
// ULIDs are 26-character strings that encode timestamp and randomness using Crockford base32.
package ulid

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ULID represents a 26-character universally unique lexicographically sortable identifier.
// Format: 10 chars timestamp (48-bit) + 16 chars randomness (80-bit) in Crockford base32.
//
//nolint:recvcheck // value receivers for getters/marshalers, pointer for UnmarshalJSON
type ULID struct {
	prefix string
	value  [26]byte
}

// Crockford base32 alphabet (no I, L, O, U to avoid confusion)
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var (
	// ErrInvalidFormat indicates the provided string is not a valid ULID format
	ErrInvalidFormat = errors.New("invalid ULID format")
	// ErrInvalidLength indicates the ULID string has incorrect length
	ErrInvalidLength = errors.New("invalid ULID length")
	// ErrInvalidCharacter indicates the ULID contains invalid base32 characters
	ErrInvalidCharacter = errors.New("invalid character in ULID")
	// ErrEntropyExhausted indicates the random source failed to provide entropy
	ErrEntropyExhausted = errors.New("failed to read random entropy")
)

// New generates a new ULID with optional prefix.
// The prefix is prepended to the ULID string (e.g., "user_01ARZ3NDEKTSV4RRFFQ69G5FAV").
func New(prefix string) ULID {
	u := ULID{prefix: prefix}

	// Generate timestamp (10 chars, 48 bits)
	now := time.Now()
	timestamp := now.UnixMilli()

	// Encode timestamp into first 10 bytes
	encodeTimestamp(&u.value, timestamp)

	// Generate random bytes (16 chars, 80 bits = 10 bytes)
	randomBytes := make([]byte, 10)
	_, err := rand.Read(randomBytes)
	if err != nil {
		// Fallback to timestamp-based pseudo-randomness if crypto/rand fails
		// This should be extremely rare
		for i := range randomBytes {
			randomBytes[i] = byte(timestamp ^ int64(i))
		}
	}

	// Encode random bytes into last 16 positions
	encodeRandom(&u.value, randomBytes)

	return u
}

// Parse parses a ULID string (with or without prefix) into a ULID value.
func Parse(s string) (ULID, error) {
	if s == "" {
		return ULID{}, ErrInvalidFormat
	}

	var prefix string
	ulidPart := s

	// Check for prefix (format: "prefix_ULID")
	if idx := strings.LastIndex(s, "_"); idx != -1 {
		prefix = s[:idx+1]
		ulidPart = s[idx+1:]
	}

	// ULID must be exactly 26 characters
	if len(ulidPart) != 26 {
		return ULID{}, ErrInvalidLength
	}

	// Validate characters
	for i := range 26 {
		c := ulidPart[i]
		if !isValidBase32Char(c) {
			return ULID{}, ErrInvalidCharacter
		}
	}

	u := ULID{prefix: prefix}
	copy(u.value[:], ulidPart)

	return u, nil
}

// String returns the string representation of the ULID with prefix if present.
func (u ULID) String() string {
	if u.prefix != "" {
		return u.prefix + string(u.value[:])
	}
	return string(u.value[:])
}

// Time extracts the timestamp from the ULID and returns it as time.Time in UTC.
func (u ULID) Time() time.Time {
	timestamp := decodeTimestamp(u.value)
	return time.UnixMilli(timestamp).UTC()
}

// Prefix returns the prefix of the ULID if present.
func (u ULID) Prefix() string {
	return u.prefix
}

// Bytes returns the 26-byte array representing the ULID value (without prefix).
func (u ULID) Bytes() [26]byte {
	return u.value
}

// MarshalJSON implements json.Marshaler interface.
func (u ULID) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (u *ULID) UnmarshalJSON(data []byte) error {
	var s string
	unmarshalErr := json.Unmarshal(data, &s)
	if unmarshalErr != nil {
		return unmarshalErr
	}

	parsed, err := Parse(s)
	if err != nil {
		return err
	}

	*u = parsed
	return nil
}

// encodeTimestamp encodes a timestamp (milliseconds since epoch) into the first 10 characters.
func encodeTimestamp(dst *[26]byte, timestamp int64) {
	// 48-bit timestamp encoded in base32 = 10 characters
	// timestamp is 64-bit, but we only use lower 48 bits (good until year 10889)
	ts := uint64(timestamp) & 0xFFFFFFFFFFFF //nolint:gosec // intentional 48-bit mask, not overflow

	// Encode from right to left
	for i := 9; i >= 0; i-- {
		dst[i] = alphabet[ts&0x1F] // 5 bits at a time
		ts >>= 5
	}
}

// encodeRandom encodes 10 bytes of random data into the last 16 characters.
func encodeRandom(dst *[26]byte, random []byte) {
	// 80 bits (10 bytes) encoded in base32 = 16 characters
	// Combine bytes into a single bit stream and encode 5 bits at a time

	bits := uint64(0)
	bitsLen := 0
	outputIdx := 10 // Start after timestamp

	for _, b := range random {
		bits = (bits << 8) | uint64(b)
		bitsLen += 8

		// Extract 5-bit chunks
		for bitsLen >= 5 {
			bitsLen -= 5
			dst[outputIdx] = alphabet[(bits>>bitsLen)&0x1F]
			outputIdx++
		}
	}

	// Handle remaining bits (should be 0 for 10 bytes = 80 bits)
	if bitsLen > 0 {
		dst[outputIdx] = alphabet[(bits<<(5-bitsLen))&0x1F]
	}
}

// decodeTimestamp decodes the first 10 characters back into a timestamp.
func decodeTimestamp(value [26]byte) int64 {
	var ts uint64

	for i := range 10 {
		ts = (ts << 5) | uint64(charToValue(value[i]))
	}

	return int64(ts) //nolint:gosec // ULID timestamps fit in int64
}

// charToValue converts a base32 character to its numeric value (0-31).
func charToValue(c byte) byte {
	// Lookup table approach for lower cyclomatic complexity
	if c >= '0' && c <= '9' {
		return c - '0'
	}
	idx := strings.IndexByte(alphabet[10:], c)
	if idx >= 0 {
		return byte(idx) + 10
	}
	return 0
}

// isValidBase32Char checks if a character is valid in Crockford base32.
func isValidBase32Char(c byte) bool {
	if c >= '0' && c <= '9' {
		return true
	}
	return strings.IndexByte(alphabet[10:], c) >= 0
}
