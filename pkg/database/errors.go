package database

import (
	"errors"
	"fmt"
)

var (
	// ErrClosed is returned when operations are attempted on a closed database
	ErrClosed = errors.New("database is closed")

	// ErrInvalidPath is returned when the database path is invalid
	ErrInvalidPath = errors.New("invalid database path")

	// ErrInvalidConfig is returned when the configuration is invalid
	ErrInvalidConfig = errors.New("invalid configuration")
)

// QueryError wraps errors that occur during query execution
type QueryError struct {
	Query string
	Err   error
}

func (e *QueryError) Error() string {
	return fmt.Sprintf("query error: %s: %v", e.Query, e.Err)
}

func (e *QueryError) Unwrap() error {
	return e.Err
}

// ExecError wraps errors that occur during command execution
type ExecError struct {
	Query string
	Err   error
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("exec error: %s: %v", e.Query, e.Err)
}

func (e *ExecError) Unwrap() error {
	return e.Err
}
