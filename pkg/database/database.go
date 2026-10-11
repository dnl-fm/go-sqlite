// Package database provides a single SQLite writer and a read-only reader pool.
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite" // Register the pure-Go database/sql driver.
)

// Database owns a single writer connection and a read-only reader pool.
type Database struct {
	reader *sql.DB
	writer *sql.DB
	config *Config
	path   string
	closed bool
	mu     sync.RWMutex
	reads  atomic.Int64
	quiet  atomic.Bool
}

// Option configures a Database before it opens.
type Option func(*Database) error

// WithConfig sets a custom configuration.
func WithConfig(cfg *Config) Option {
	return func(d *Database) error {
		if cfg == nil {
			return ErrInvalidConfig
		}
		copyConfig := *cfg
		copyConfig.Pragmas = maps.Clone(cfg.Pragmas)
		copyConfig.PragmaOrder = slices.Clone(cfg.PragmaOrder)
		d.config = &copyConfig
		return nil
	}
}

// Open opens path in WAL mode. In-memory paths use one shared connection.
func Open(ctx context.Context, path string, opts ...Option) (*Database, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	d := &Database{path: path, config: DefaultConfig()}
	for _, opt := range opts {
		if err := opt(d); err != nil {
			return nil, err
		}
	}
	target, memory, err := databaseTarget(path)
	if err != nil {
		return nil, err
	}
	if !memory {
		if legacyErr := rejectLegacyFile(target.Path); legacyErr != nil {
			return nil, legacyErr
		}
	}
	pragmas, err := checkedPragmas(ctx, target, d.config)
	if err != nil {
		return nil, err
	}
	d.writer, err = sql.Open("sqlite", connectionDSN(target, pragmas, false))
	if err != nil {
		return nil, err
	}
	d.writer.SetMaxOpenConns(1)
	d.writer.SetMaxIdleConns(1)
	d.writer.SetConnMaxLifetime(0)
	var mode string
	err = d.writer.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
	if err == nil && !memory && !strings.EqualFold(mode, walJournalMode) {
		err = fmt.Errorf("journal_mode is %q, want %s", mode, walJournalMode)
	}
	if err != nil {
		return nil, errors.Join(err, d.writer.Close())
	}
	if memory {
		d.reader = d.writer
		return d, nil
	}
	d.reader, err = sql.Open("sqlite", connectionDSN(target, pragmas, true))
	if err != nil {
		return nil, errors.Join(err, d.writer.Close())
	}
	readers := d.config.MaxOpenConns
	if readers <= 0 {
		readers = 4
	}
	d.reader.SetMaxOpenConns(readers)
	d.reader.SetMaxIdleConns(d.config.MaxIdleConns)
	d.reader.SetConnMaxLifetime(d.config.ConnMaxLifetime)
	if err := d.reader.PingContext(ctx); err != nil {
		return nil, errors.Join(err, d.reader.Close(), d.writer.Close())
	}
	return d, nil
}

// Query executes a query that returns rows
func (d *Database) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	rows, err := d.reader.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // GO-SQLITE-1: caller owns row closing
	if err != nil {
		return nil, &QueryError{Query: query, Err: err}
	}

	return rows, nil
}

// QueryOne executes a query that returns a single row.
// Note: does not check closed state — errors surface at Scan() time.
// This matches Go's sql.DB.QueryRow pattern where errors are deferred.
func (d *Database) QueryOne(ctx context.Context, query string, args ...any) *sql.Row {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.reader.QueryRowContext(ctx, query, args...)
}

// Exec executes a command that doesn't return rows
func (d *Database) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	result, err := d.writer.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, &ExecError{Query: query, Err: err}
	}

	return result, nil
}

// BeginTx starts a transaction with the given options
func (d *Database) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	pool := d.writer
	if opts != nil && opts.ReadOnly {
		pool = d.reader
	}
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	return tx, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.closed {
		return ErrClosed
	}

	d.closed = true
	if d.reader == d.writer {
		return d.writer.Close()
	}
	return errors.Join(d.reader.Close(), d.writer.Close())
}

// Path returns the database file path
func (d *Database) Path() string {
	return d.path
}

// Config returns the configuration supplied to Open.
func (d *Database) Config() *Config { return d.config }

// DB returns the writer; generic consumers can safely use it for reads or writes.
func (d *Database) DB() *sql.DB { return d.writer }

// Writer returns the single-connection write pool.
func (d *Database) Writer() *sql.DB { return d.writer }

// Reader returns the read-only pool (the shared writer for in-memory paths).
func (d *Database) Reader() *sql.DB { return d.reader }
