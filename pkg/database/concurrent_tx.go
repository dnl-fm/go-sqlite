package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ConnTx is the query surface available inside a concurrent transaction.
type ConnTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ConcurrentTransaction is a manually committed BEGIN CONCURRENT transaction.
type ConcurrentTransaction struct {
	conn   *sql.Conn
	closed bool
}

// BeginConcurrentTx starts a Turso BEGIN CONCURRENT transaction and returns a
// transaction handle with a sql.Tx-like query/commit/rollback surface.
func BeginConcurrentTx(ctx context.Context, db *sql.DB) (*ConcurrentTransaction, error) {
	if db == nil {
		return nil, ErrInvalidConfig
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to reserve connection: %w", err)
	}
	if _, err := conn.ExecContext(ctx, "BEGIN CONCURRENT"); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to begin concurrent transaction: %w", err)
	}
	return &ConcurrentTransaction{conn: conn}, nil
}

// ExecContext executes a statement inside the concurrent transaction.
func (tx *ConcurrentTransaction) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if tx == nil || tx.conn == nil || tx.closed {
		return nil, sql.ErrTxDone
	}
	return tx.conn.ExecContext(ctx, query, args...)
}

// Exec executes a statement inside the concurrent transaction.
func (tx *ConcurrentTransaction) Exec(query string, args ...any) (sql.Result, error) {
	return tx.ExecContext(context.Background(), query, args...)
}

// QueryContext executes a query inside the concurrent transaction.
func (tx *ConcurrentTransaction) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if tx == nil || tx.conn == nil || tx.closed {
		return nil, sql.ErrTxDone
	}
	return tx.conn.QueryContext(ctx, query, args...)
}

// Query executes a query inside the concurrent transaction.
func (tx *ConcurrentTransaction) Query(query string, args ...any) (*sql.Rows, error) {
	return tx.QueryContext(context.Background(), query, args...)
}

// QueryRowContext executes a single-row query inside the concurrent transaction.
func (tx *ConcurrentTransaction) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if tx == nil || tx.conn == nil || tx.closed {
		return &sql.Row{}
	}
	return tx.conn.QueryRowContext(ctx, query, args...)
}

// QueryRow executes a single-row query inside the concurrent transaction.
func (tx *ConcurrentTransaction) QueryRow(query string, args ...any) *sql.Row {
	return tx.QueryRowContext(context.Background(), query, args...)
}

// PrepareContext prepares a statement on the reserved transaction connection.
func (tx *ConcurrentTransaction) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	if tx == nil || tx.conn == nil || tx.closed {
		return nil, sql.ErrTxDone
	}
	return tx.conn.PrepareContext(ctx, query)
}

// Commit commits the concurrent transaction and releases its reserved connection.
func (tx *ConcurrentTransaction) Commit() error {
	if tx == nil || tx.conn == nil || tx.closed {
		return sql.ErrTxDone
	}
	tx.closed = true
	_, err := tx.conn.ExecContext(context.Background(), "COMMIT")
	closeErr := tx.conn.Close()
	if err != nil {
		return fmt.Errorf("failed to commit concurrent transaction: %w", err)
	}
	return closeErr
}

// Rollback rolls back the concurrent transaction and releases its reserved connection.
func (tx *ConcurrentTransaction) Rollback() error {
	if tx == nil || tx.conn == nil || tx.closed {
		return sql.ErrTxDone
	}
	tx.closed = true
	err := rollbackConcurrent(context.Background(), tx.conn)
	closeErr := tx.conn.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// ConcurrentTx executes fn inside a Turso BEGIN CONCURRENT transaction.
//
// The database must use Turso MVCC mode, for example by opening it with
// WithTursoMVCC. ConcurrentTx reserves one *sql.Conn so BEGIN CONCURRENT,
// all callback statements, and COMMIT run on the same underlying connection.
func ConcurrentTx(ctx context.Context, db *sql.DB, fn func(ConnTx) error) error {
	if db == nil {
		return ErrInvalidConfig
	}
	if fn == nil {
		return ErrInvalidConfig
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to reserve connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN CONCURRENT"); err != nil {
		return fmt.Errorf("failed to begin concurrent transaction: %w", err)
	}

	if err := fn(conn); err != nil {
		rbErr := rollbackConcurrent(ctx, conn)
		if rbErr != nil {
			return fmt.Errorf("rollback failed: %w (original error: %w)", rbErr, err)
		}
		return err
	}

	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		if rbErr := rollbackConcurrent(ctx, conn); rbErr != nil {
			return fmt.Errorf("rollback after commit failure failed: %w (commit error: %w)", rbErr, err)
		}
		return fmt.Errorf("failed to commit concurrent transaction: %w", err)
	}

	return nil
}

// ConcurrentTxRetry executes ConcurrentTx and retries optimistic MVCC conflicts.
func ConcurrentTxRetry(ctx context.Context, db *sql.DB, attempts int, fn func(ConnTx) error) error {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		err := ConcurrentTx(ctx, db, fn)
		if err == nil {
			return nil
		}
		lastErr = err
		if !IsRetryableConcurrentTxError(err) || attempt == attempts {
			break
		}

		delay := time.Duration(attempt) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("concurrent transaction retry canceled: %w", ctx.Err())
		case <-timer.C:
		}
	}

	return lastErr
}

// ConcurrentTx executes fn inside a Turso BEGIN CONCURRENT transaction.
func (d *Database) ConcurrentTx(ctx context.Context, fn func(ConnTx) error) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return ErrClosed
	}

	return ConcurrentTx(ctx, d.db, fn)
}

// ConcurrentTxRetry executes ConcurrentTx and retries optimistic MVCC conflicts.
func (d *Database) ConcurrentTxRetry(ctx context.Context, attempts int, fn func(ConnTx) error) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return ErrClosed
	}

	return ConcurrentTxRetry(ctx, d.db, attempts, fn)
}

// BeginConcurrentTx starts a manually committed BEGIN CONCURRENT transaction.
func (d *Database) BeginConcurrentTx(ctx context.Context) (*ConcurrentTransaction, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.closed {
		return nil, ErrClosed
	}

	return BeginConcurrentTx(ctx, d.db)
}

// IsRetryableConcurrentTxError reports whether err is a Turso MVCC conflict or busy error.
func IsRetryableConcurrentTxError(err error) bool {
	for current := err; current != nil; current = errors.Unwrap(current) {
		msg := strings.ToLower(current.Error())
		if strings.Contains(msg, "busy") ||
			strings.Contains(msg, "locked") ||
			strings.Contains(msg, "conflict") ||
			strings.Contains(msg, "busy_snapshot") {
			return true
		}
	}
	return false
}

func rollbackConcurrent(ctx context.Context, conn *sql.Conn) error {
	_, err := conn.ExecContext(ctx, "ROLLBACK")
	return err
}
