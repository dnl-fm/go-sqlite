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
