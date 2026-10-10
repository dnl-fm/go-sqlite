// Package writers is a copyable reference for several writers against one
// SQLite database in WAL mode with modernc.org/sqlite.
//
// SQLite WAL allows exactly one writer at a time and any number of concurrent
// readers. The patterns here make that rule explicit instead of discovering it
// as "database is locked" in production:
//
//   - Store (this file): one writer connection, a separate read-only reader
//     pool. Writers wait in Go, on database/sql's pool, never inside SQLite's
//     busy handler.
//   - Queue (queue.go): group commit on top of Store for many tiny writes,
//     in strict arrival order.
package writers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // Register the pure-Go SQLite database/sql driver.
)

// Options tunes Open. The zero value is the recommended default.
type Options struct {
	// Readers is the reader pool size. Default 4.
	Readers int
	// Synchronous is the PRAGMA synchronous level. Default "NORMAL": a commit
	// survives an application crash, but the last commits can be lost on OS
	// crash or power loss (the database stays consistent). Use "FULL" when an
	// acknowledged write must survive power loss; every commit then pays an
	// fsync, which is what Queue exists to amortize.
	Synchronous string
}

// Store is one database file opened the only way WAL wants it: a single
// writer connection and a pool of read-only connections.
type Store struct {
	// Reader is a read-only pool. Writes through it fail with SQLITE_READONLY.
	Reader *sql.DB
	// Writer has exactly one connection and starts every transaction with
	// BEGIN IMMEDIATE. Never hold its transaction open across slow work.
	Writer *sql.DB

	// Checkpoint's quiet window: Read counts itself in and stays out while
	// the window is closed.
	reads  atomic.Int64
	closed atomic.Bool
}

// busyTimeout is how long a connection waits for a lock, in milliseconds.
const busyTimeout = 5000

// Open opens path (creating it if needed) in WAL mode.
//
// Every pragma travels in the DSN, so modernc applies it to every connection
// the pool ever opens, including replacements. Running PRAGMA statements on a
// *sql.DB instead configures one random pooled connection.
func Open(ctx context.Context, path string, opts Options) (*Store, error) {
	if opts.Readers <= 0 {
		opts.Readers = 4
	}
	if opts.Synchronous == "" {
		opts.Synchronous = "NORMAL"
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	writer, err := sql.Open("sqlite", dsn(path, opts.Synchronous, false))
	if err != nil {
		return nil, err
	}
	// One connection, kept forever: writers wait for it in database/sql
	// (context-aware, no polling) and can never hit SQLITE_BUSY on each other.
	// database/sql wakes a random waiter, not the oldest; use Queue when
	// arrival order or a tight latency tail matters.
	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)

	// The writer goes first: it creates the file and switches it to WAL, which
	// the read-only connections cannot do.
	var mode string
	err = writer.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode)
	if err == nil && !strings.EqualFold(mode, "wal") {
		err = fmt.Errorf("writers: journal_mode is %q, want wal", mode)
	}
	if err != nil {
		_ = writer.Close()
		return nil, err
	}

	reader, err := sql.Open("sqlite", dsn(path, opts.Synchronous, true))
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(opts.Readers)
	reader.SetMaxIdleConns(opts.Readers)
	reader.SetConnMaxLifetime(0)
	err = reader.PingContext(ctx)
	if err != nil {
		_ = reader.Close()
		_ = writer.Close()
		return nil, err
	}
	return &Store{Reader: reader, Writer: writer}, nil
}

// dsn builds the modernc DSN. Readers are opened with mode=ro, which SQLite
// enforces below the SQL layer: unlike PRAGMA query_only it cannot be switched
// off by a statement. Readers carry no _txlock, so their transactions are
// plain deferred (read) transactions.
func dsn(path, synchronous string, readOnly bool) string {
	params := url.Values{}
	for _, pragma := range []string{
		"journal_mode=WAL",
		"synchronous=" + synchronous,
		fmt.Sprintf("busy_timeout=%d", busyTimeout),
		"foreign_keys=ON",
		"temp_store=MEMORY",
	} {
		params.Add("_pragma", pragma)
	}
	if readOnly {
		params.Set("mode", "ro")
	} else {
		params.Set("_txlock", "immediate")
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: params.Encode()}).String()
}

// Close closes the reader pool first, so the writer is the last connection on
// the file and performs the final checkpoint (read-only connections cannot).
func (s *Store) Close() error {
	return errors.Join(s.Reader.Close(), s.Writer.Close())
}

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
func (s *Store) Write(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	defer func() {
		if errors.Is(err, sql.ErrTxDone) && ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	tx, err := s.Writer.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback() // error or panic; a panic keeps unwinding after this
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
func (s *Store) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	// Step in; if a checkpoint has closed the window, step back out and wait.
	for s.reads.Add(1); s.closed.Load(); s.reads.Add(1) {
		s.reads.Add(-1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	defer s.reads.Add(-1)
	tx, err := s.Reader.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // nothing to commit in a read
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
// growing, but writers lose quiet per call, not busyTimeout.
//
// Like Write, never call it from inside a Write callback.
func (s *Store) Checkpoint(ctx context.Context, quiet time.Duration) (busy bool, err error) {
	// Take the writer first: closing the window while a long Write holds it
	// would stall new reads for that whole Write, not just for quiet.
	conn, err := s.Writer.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	s.closed.Store(true)
	defer s.closed.Store(false)
	for deadline := time.Now().Add(quiet); s.reads.Load() > 0 && time.Now().Before(deadline); {
		time.Sleep(time.Millisecond)
	}
	// A TRUNCATE checkpoint holds the write lock while it waits for readers,
	// for as long as the connection's busy_timeout allows.
	_, err = conn.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout=%d", quiet.Milliseconds()))
	if err != nil {
		return false, err
	}
	var b, log, done int
	err = conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&b, &log, &done)
	_, restore := conn.ExecContext(context.WithoutCancel(ctx), fmt.Sprintf("PRAGMA busy_timeout=%d", busyTimeout))
	return b != 0, errors.Join(err, restore)
}
