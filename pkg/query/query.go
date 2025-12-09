// Package query provides SQL query building with named parameters.
// Queries use :name placeholder syntax which is converted to ? for execution.
package query

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrEmptySQL is returned when SQL string is empty
	ErrEmptySQL = errors.New("query: SQL string cannot be empty")
	// ErrMissingParam is returned when a required parameter is not provided
	ErrMissingParam = errors.New("query: missing required parameter")
	// ErrExtraParam is returned when an unused parameter is provided
	ErrExtraParam = errors.New("query: unused parameter provided")
)

// paramPattern matches :paramName placeholders
var paramPattern = regexp.MustCompile(`:([a-zA-Z_][a-zA-Z0-9_]*)`)

// Query represents a SQL query with named parameters.
// Parameters use :name syntax in the original SQL and are converted
// to ? placeholders for safe execution.
type Query struct {
	original string         // Original SQL with :name placeholders
	sql      string         // Converted SQL with ? placeholders
	args     []any          // Ordered arguments matching ? positions
	params   map[string]any // Original named parameters
}

// Build creates a Query with named parameters.
// SQL uses :name placeholders, params provides values.
// The SQL is converted to use ? placeholders for execution.
//
// Example:
//
//	q, err := query.Build(
//	    "SELECT * FROM users WHERE email = :email AND active = :active",
//	    map[string]any{"email": "alice@test.com", "active": true},
//	)
//	rows, err := db.Query(q.SQL(), q.Args()...)
func Build(sqlStr string, params map[string]any) (*Query, error) {
	if sqlStr == "" {
		return nil, ErrEmptySQL
	}

	// Handle nil params as empty map
	if params == nil {
		params = make(map[string]any)
	}

	// Find all placeholders in order
	matches := paramPattern.FindAllStringSubmatchIndex(sqlStr, -1)

	// Collect unique placeholder names for validation
	placeholderSet := make(map[string]bool)
	for _, match := range matches {
		name := sqlStr[match[2]:match[3]]
		placeholderSet[name] = true
	}

	// Validate all placeholders have values
	for name := range placeholderSet {
		if _, exists := params[name]; !exists {
			return nil, fmt.Errorf("%w: %s", ErrMissingParam, name)
		}
	}

	// Check for unused params (helps catch typos)
	for name := range params {
		if !placeholderSet[name] {
			return nil, fmt.Errorf("%w: %s", ErrExtraParam, name)
		}
	}

	// Convert :name to ? and build ordered args
	var builder strings.Builder
	args := make([]any, 0, len(matches))
	lastPos := 0

	for _, match := range matches {
		// match[0], match[1] = full match positions (:name)
		// match[2], match[3] = capture group positions (name)
		name := sqlStr[match[2]:match[3]]

		// Append SQL before the placeholder
		builder.WriteString(sqlStr[lastPos:match[0]])

		// Replace with ?
		builder.WriteRune('?')

		// Add value to args
		args = append(args, params[name])

		lastPos = match[1]
	}

	// Append remaining SQL
	builder.WriteString(sqlStr[lastPos:])

	return &Query{
		original: sqlStr,
		sql:      builder.String(),
		args:     args,
		params:   params,
	}, nil
}

// New creates a Query without parameters.
// Use for simple queries like "SELECT * FROM users".
// Returns error if SQL contains :name placeholders.
//
// Example:
//
//	q, err := query.New("SELECT * FROM users")
//	rows, err := db.Query(q.SQL(), q.Args()...)
func New(sqlStr string) (*Query, error) {
	if sqlStr == "" {
		return nil, ErrEmptySQL
	}

	// Check for accidental placeholders
	if matches := paramPattern.FindStringSubmatch(sqlStr); len(matches) > 0 {
		return nil, fmt.Errorf("query contains placeholder :%s, use Build() instead", matches[1])
	}

	return &Query{
		original: sqlStr,
		sql:      sqlStr,
		args:     []any{},
		params:   make(map[string]any),
	}, nil
}

// SQL returns the SQL string with ? placeholders, ready for execution.
func (q *Query) SQL() string {
	return q.sql
}

// Args returns ordered arguments matching the ? placeholders.
func (q *Query) Args() []any {
	return q.args
}

// Params returns the original named parameter map.
func (q *Query) Params() map[string]any {
	return q.params
}

// Original returns the original SQL with :name placeholders.
func (q *Query) Original() string {
	return q.original
}

// String implements fmt.Stringer, returns the executable SQL.
func (q *Query) String() string {
	return q.sql
}
