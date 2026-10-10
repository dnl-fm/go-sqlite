package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const mvccJournalMode = "mvcc"

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

// WithDriver is kept for source compatibility. go-sqlite always uses Turso MVCC.
func WithDriver(name string) Option {
	return func(d *Database) error {
		d.config.Driver = DefaultDriver
		d.config.WithPragma("journal_mode", "'mvcc'")
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
	db.config.Driver = DefaultDriver
	db.config.WithPragma("journal_mode", "'mvcc'")

	if db.config.EnableMultiprocessWAL {
		return nil, errors.New("multiprocess WAL is incompatible with go-sqlite MVCC")
	}
	if containsMultiprocessWAL(path) {
		return nil, errors.New("DSN must not request experimental=multiprocess_wal with go-sqlite MVCC")
	}

	// Resolve the registered driver without opening a native connection.
	registeredDB, err := sql.Open(db.config.Driver, path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}
	registeredDriver := registeredDB.Driver()
	err = registeredDB.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to close driver lookup: %w", err)
	}
	sqlDB := sql.OpenDB(&pragmaConnector{driver: registeredDriver, path: path, config: db.config})

	db.db = sqlDB

	// Configure connection pool
	db.db.SetMaxOpenConns(db.config.MaxOpenConns)
	db.db.SetMaxIdleConns(db.config.MaxIdleConns)
	db.db.SetConnMaxLifetime(db.config.ConnMaxLifetime)

	// Verify connection
	err = db.db.PingContext(ctx)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}
	if err := ValidateRowIDSchema(ctx, db.db); err != nil {
		sqlDB.Close()
		return nil, err
	}

	return db, nil
}

// pragmaConnector configures every native connection before the pool uses it.
type pragmaConnector struct {
	config *Config
	driver driver.Driver
	path   string
}

func (c *pragmaConnector) Driver() driver.Driver { return c.driver }

func (c *pragmaConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := c.driver.Open(c.path)
	if err != nil {
		return nil, err
	}
	if err := c.applyPragmas(ctx, conn); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (c *pragmaConnector) applyPragmas(ctx context.Context, conn driver.Conn) error {
	applied := make(map[string]bool, len(c.config.Pragmas))
	for _, key := range c.config.PragmaOrder {
		value, ok := c.config.Pragmas[key]
		if !ok {
			continue
		}
		if err := c.applyPragma(ctx, conn, key, value); err != nil {
			return err
		}
		applied[key] = true
	}

	var remaining []string
	for key := range c.config.Pragmas {
		if !applied[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	for _, key := range remaining {
		if err := c.applyPragma(ctx, conn, key, c.config.Pragmas[key]); err != nil {
			return err
		}
	}
	return nil
}

func (c *pragmaConnector) applyPragma(ctx context.Context, conn driver.Conn, key, value string) error {
	query := fmt.Sprintf("PRAGMA %s = %s", key, value)
	if isMVCCJournalModePragma(key, value) {
		return verifyMVCCJournalMode(ctx, conn, query)
	}

	execer, ok := conn.(driver.ExecerContext)
	var err error
	if !ok {
		err = errors.New("driver does not implement ExecerContext")
	} else {
		_, err = execer.ExecContext(ctx, query, nil)
	}
	if err != nil {
		if c.config.StrictPragmas {
			return fmt.Errorf("set pragma %q: %w", query, err)
		}
		// Ignore pragma errors for compatibility. Some SQLite drivers or in-memory
		// databases do not support every pragma.
	}
	return nil
}

func verifyMVCCJournalMode(ctx context.Context, conn driver.Conn, query string) error {
	queryer, ok := conn.(driver.QueryerContext)
	if !ok {
		return fmt.Errorf("set pragma %q: driver does not implement QueryerContext", query)
	}
	rows, err := queryer.QueryContext(ctx, query, nil)
	if err != nil {
		return fmt.Errorf("set pragma %q: %w", query, err)
	}
	values := make([]driver.Value, len(rows.Columns()))
	err = rows.Next(values)
	closeErr := rows.Close()
	if err != nil {
		return fmt.Errorf("set pragma %q: %w", query, err)
	}
	if closeErr != nil {
		return fmt.Errorf("set pragma %q: %w", query, closeErr)
	}
	if len(values) != 1 {
		return fmt.Errorf("set pragma %q: expected one journal_mode column, got %d", query, len(values))
	}
	mode := fmt.Sprint(values[0])
	if b, ok := values[0].([]byte); ok {
		mode = string(b)
	}
	if !strings.EqualFold(mode, mvccJournalMode) {
		return fmt.Errorf("set pragma %q: expected journal_mode %s, got %q", query, mvccJournalMode, mode)
	}
	return nil
}

// Query executes a query that returns rows
func (d *Database) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	rows, err := d.db.QueryContext(ctx, query, args...) //nolint:sqlclosecheck // GO-SQLITE-1: caller owns row closing
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
	if err := rejectWithoutRowIDSQL(query); err != nil {
		return nil, &ExecError{Query: query, Err: err}
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

func containsMultiprocessWAL(path string) bool {
	_, rawQuery, ok := strings.Cut(path, "?")
	if !ok {
		return false
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if !strings.EqualFold(key, "experimental") {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(value, "multiprocess_wal") {
				return true
			}
		}
	}
	return false
}

func isMVCCJournalModePragma(key, value string) bool {
	if !strings.EqualFold(key, "journal_mode") {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.Trim(normalized, `"'`)
	return normalized == mvccJournalMode
}

// Config returns the current configuration
func (d *Database) Config() *Config {
	return d.config
}

// DB returns the underlying sql.DB (for advanced usage)
func (d *Database) DB() *sql.DB {
	return d.db
}
