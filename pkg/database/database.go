package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// Database wraps sql.DB and provides context-aware operations
type Database struct {
	db     *sql.DB
	config *Config
	path   string
	closed bool
	mu     sync.RWMutex
}

// Option is a functional option for configuring the Database
type Option func(*Database) error

// WithDriver sets the database/sql driver name as a functional option.
// Empty name is ignored (keeps current driver).
func WithDriver(name string) Option {
	return func(d *Database) error {
		if name != "" {
			d.config.Driver = name
		}
		return nil
	}
}

// WithConfig sets a custom configuration
func WithConfig(cfg *Config) Option {
	return func(d *Database) error {
		if cfg == nil {
			return ErrInvalidConfig
		}
		d.config = cfg
		return nil
	}
}

// Open creates a new Database connection with the given options
func Open(ctx context.Context, path string, opts ...Option) (*Database, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}

	db := &Database{
		path:   path,
		config: DefaultConfig(),
		closed: false,
	}

	// Apply options
	for _, opt := range opts {
		err := opt(db)
		if err != nil {
			return nil, err
		}
	}

	// Open database connection using configured driver
	sqlDB, err := sql.Open(db.config.Driver, path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.db = sqlDB

	// Configure connection pool
	db.db.SetMaxOpenConns(db.config.MaxOpenConns)
	db.db.SetMaxIdleConns(db.config.MaxIdleConns)
	db.db.SetConnMaxLifetime(db.config.ConnMaxLifetime)

	// Apply pragmas
	db.applyPragmas(ctx)

	// Verify connection
	err = db.db.PingContext(ctx)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// applyPragmas applies configured pragma settings
func (d *Database) applyPragmas(ctx context.Context) {
	for key, value := range d.config.Pragmas {
		query := fmt.Sprintf("PRAGMA %s = %s", key, value)
		_, err := d.db.ExecContext(ctx, query)
		if err != nil {
			// Ignore pragma errors for compatibility (some pragmas may not be supported)
			// This is especially true for in-memory databases or different SQLite versions
			continue
		}
	}
}

// Query executes a query that returns rows
func (d *Database) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	rows, err := d.db.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // caller is responsible for closing rows
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

	return d.db.QueryRowContext(ctx, query, args...)
}

// Exec executes a command that doesn't return rows
func (d *Database) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	result, err := d.db.ExecContext(ctx, query, args...)
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

	tx, err := d.db.BeginTx(ctx, opts)
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
	return d.db.Close()
}

// Path returns the database file path
func (d *Database) Path() string {
	return d.path
}

// Config returns the current configuration
func (d *Database) Config() *Config {
	return d.config
}

// DB returns the underlying sql.DB (for advanced usage)
func (d *Database) DB() *sql.DB {
	return d.db
}
