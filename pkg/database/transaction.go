package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Write runs fn in a write transaction on the single writer connection. It
// commits when fn returns nil and rolls back when fn returns an error or
// panics (the panic then continues to propagate).
// If context cancellation automatically rolls back the transaction, Write
// returns the context error rather than database/sql's ErrTxDone.
//
// The transaction is BEGIN IMMEDIATE, so a SELECT followed by an UPDATE inside
// fn is a safe read-modify-write: nobody else can write in between.
//
// fn must use only the tx it is given. Calling Write again from inside fn
// waits forever: there is one writer connection and fn is holding it.
func (d *Database) Write(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	defer func() {
		if errors.Is(err, sql.ErrTxDone) && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	tx, err := d.writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback() //nolint:errcheck // GO-SQLITE-1: best-effort rollback on error or panic.
		}
	}()
	err = fn(tx)
	if err != nil {
		return err
	}
	err = tx.Commit()
	committed = err == nil
	return err
}

// Read runs fn in a read-only transaction on the reader pool: every statement
// in fn sees the same snapshot. Keep fn short; an open read transaction pins
// the WAL and stops checkpoints from reclaiming it.
func (d *Database) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	// Step in; if a checkpoint has closed the window, step back out and wait.
	for d.reads.Add(1); d.quiet.Load(); d.reads.Add(1) {
		d.reads.Add(-1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer d.reads.Add(-1)
	tx, err := d.reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // GO-SQLITE-1: read-only transaction cleanup.
	return fn(tx)
}

// Checkpoint copies the WAL into the database and empties it, inside a quiet
// window. SQLite's own checkpoint can only start the WAL over at a moment when
// no reader is on an older snapshot. Long reads that overlap never leave such
// a moment, so the WAL grows for as long as they keep coming. If you have such
// reads, send them through Read and call Checkpoint from one timer.
//
// It takes the writer connection first, so nothing commits meanwhile. New Read
// calls then wait while the running ones get up to quiet to finish. The
// checkpoint runs on that writer connection and waits about quiet again for a
// read that is still running or was never behind the window: a query on
// Reader, another process. busy reports that
// one was still there and the WAL was not emptied this time. A read
// transaction that never ends makes every call busy and the WAL keeps
// growing, but writers lose quiet per call, not the configured busy timeout.
//
// Like Write, never call it from inside a Write callback.
func (d *Database) Checkpoint(ctx context.Context, quiet time.Duration) (bool, error) {
	// Take the writer first: closing the window while a long Write holds it
	// would stall new reads for that whole Write, not just for quiet.
	conn, err := d.writer.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	d.quiet.Store(true)
	defer d.quiet.Store(false)
	for deadline := time.Now().Add(quiet); d.reads.Load() > 0 && time.Now().Before(deadline); {
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	// A TRUNCATE checkpoint holds the write lock while it waits for readers,
	// for as long as the connection's busy_timeout allows.
	var previousTimeout int
	if readErr := conn.QueryRowContext(ctx, "PRAGMA "+busyTimeoutKey).Scan(&previousTimeout); readErr != nil {
		return false, readErr
	}
	_, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", quiet.Milliseconds()))
	if err != nil {
		return false, err
	}
	var b, log, done int
	err = conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&b, &log, &done)
	_, restore := conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("PRAGMA busy_timeout=%d", previousTimeout))
	return b != 0, errors.Join(err, restore)
}
