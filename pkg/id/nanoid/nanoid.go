// Package nanoid provides NanoID generation for compact, URL-safe unique identifiers.
// NanoIDs use a 64-character alphabet and cryptographically random generation.
package nanoid

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
)

// NanoID represents a compact, URL-safe unique identifier.
// Default length is 21 characters using alphabet: _-0-9a-zA-Z
//
//nolint:recvcheck // value receivers for getters/marshalers, pointer for UnmarshalJSON
type NanoID struct {
	value string
}

// URL-safe alphabet: 64 characters
const alphabet = "_-0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

const (
	// DefaultLength is the default NanoID length (21 chars = ~118 bits entropy)
	DefaultLength = 21
	// MinLength is the minimum allowed length
	MinLength = 6
	// MaxLength is the maximum allowed length
	MaxLength = 255
)

var (
	// ErrInvalidFormat indicates the provided string is not a valid NanoID
	ErrInvalidFormat = errors.New("invalid NanoID format")
	// ErrInvalidLength indicates the NanoID has invalid length
	ErrInvalidLength = errors.New("invalid NanoID length: must be between 6 and 255")
	// ErrInvalidCharacter indicates the NanoID contains invalid characters
	ErrInvalidCharacter = errors.New("invalid character in NanoID")
)

// New generates a new NanoID with default length (21 characters).
func New() NanoID {
	return NewWithLength(DefaultLength)
}

// NewWithLength generates a new NanoID with specified length.
// Length must be between MinLength (6) and MaxLength (255).
func NewWithLength(length int) NanoID {
	if length < MinLength {
		length = MinLength
	}
	if length > MaxLength {
		length = MaxLength
	}

	// Generate random bytes
	randomBytes := make([]byte, length)
	_, err := rand.Read(randomBytes)
	if err != nil {
		panic("nanoid: crypto/rand failed: " + err.Error())
	}

	// Map random bytes to alphabet
	result := make([]byte, length)
	for i := range length {
		// Use modulo to map byte (0-255) to alphabet index (0-63)
		// This has slight bias but is acceptable for NanoID use case
		result[i] = alphabet[int(randomBytes[i])&63] // &63 is same as %64 for powers of 2
	}

	return NanoID{value: string(result)}
}

// Parse parses a NanoID string into a NanoID value.
func Parse(s string) (NanoID, error) {
	if s == "" {
		return NanoID{}, ErrInvalidFormat
	}

	if len(s) < MinLength || len(s) > MaxLength {
		return NanoID{}, ErrInvalidLength
	}

	// Validate all characters are in alphabet
	for i := range len(s) {
		if !strings.ContainsRune(alphabet, rune(s[i])) {
			return NanoID{}, ErrInvalidCharacter
		}
	}

	return NanoID{value: s}, nil
}

// String returns the string representation of the NanoID.
func (n NanoID) String() string {
	return n.value
}

// Bytes returns the byte slice representation of the NanoID.
func (n NanoID) Bytes() []byte {
	return []byte(n.value)
}

// MarshalJSON implements json.Marshaler interface.
func (n NanoID) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.value)
}

// UnmarshalJSON implements json.Unmarshaler interface.
func (n *NanoID) UnmarshalJSON(data []byte) error {
	var s string
	unmarshalErr := json.Unmarshal(data, &s)
	if unmarshalErr != nil {
		return unmarshalErr
	}

	parsed, err := Parse(s)
	if err != nil {
		return err
	}

	*n = parsed
	return nil
}
