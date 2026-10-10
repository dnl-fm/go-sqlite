package tenants

import (
	"container/list"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"

	"modernc.org/sqlite"
)

// readerConns is the size of each tenant's reader pool.
const readerConns = 4

// Tenant is one open tenant database: a single-connection writer and a small
// reader pool over the same WAL file. It is only valid inside Manager.With.
type Tenant struct {
	id   string
	path string
	w, r *sql.DB

	// Cache state, guarded by Manager.mu.
	refs    int
	idle    *list.Element // position in Manager.idle while refs == 0
	opening bool          // handles are being opened; wait
	doomed  bool          // Delete claimed it; no new users
}

// ID returns the tenant ID.
func (t *Tenant) ID() string { return t.id }

// Write runs fn in a BEGIN IMMEDIATE transaction on the tenant's only writer
// connection. Writes to one tenant queue here; they never see SQLITE_BUSY.
func (t *Tenant) Write(ctx context.Context, fn func(*sql.Tx) error) error {
	return inTx(ctx, t.w, fn)
}

// Read runs fn in a snapshot transaction on a query_only reader connection.
// Readers do not block the writer and the writer does not block them.
func (t *Tenant) Read(ctx context.Context, fn func(*sql.Tx) error) error {
	return inTx(ctx, t.r, fn)
}

func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit; frees the connection if fn panics
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// openDBs applies the same WAL policy to every connection, including
// replacement connections. mode=rw makes SQLite refuse to create a missing
// file, so opening can never invent a tenant.
func openDBs(ctx context.Context, path string) (w, r *sql.DB, err error) {
	dsn := func(extra, value string) string {
		q := url.Values{"mode": {"rw"}, extra: {value}}
		for _, pragma := range []string{"journal_mode=WAL", "synchronous=NORMAL", "busy_timeout=5000", "foreign_keys=ON"} {
			q.Add("_pragma", pragma)
		}
		return (&url.URL{Scheme: "file", Path: path, RawQuery: q.Encode()}).String()
	}
	w, err = sql.Open("sqlite", dsn("_txlock", "immediate"))
	if err != nil {
		return nil, nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err = w.PingContext(ctx); err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	r, err = sql.Open("sqlite", dsn("_query_only", "1")) // connects lazily, on first Read
	if err != nil {
		_ = w.Close()
		return nil, nil, err
	}
	r.SetMaxOpenConns(readerConns)
	return w, r, nil
}

// close closes the readers first so the writer is the last connection out:
// the last close checkpoints the WAL and removes the -wal and -shm files.
func (t *Tenant) close() error {
	if t.w == nil {
		return nil
	}
	return errors.Join(t.r.Close(), t.w.Close())
}

// migrate brings the database to version len(steps). A step and its version
// bump commit together, so a failing step leaves the tenant where it was.
func migrate(ctx context.Context, id string, w *sql.DB, steps []string) error {
	var v int
	if err := w.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(steps) {
		return &MigrationError{Tenant: id, Version: v, Err: fmt.Errorf("schema is newer than this binary (knows %d)", len(steps))}
	}
	for ; v < len(steps); v++ {
		err := inTx(ctx, w, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, steps[v]); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v+1))
			return err
		})
		if err != nil {
			return &MigrationError{Tenant: id, Version: v, Err: err}
		}
	}
	return nil
}

// backup copies the tenant into dst with SQLite's online backup API. One
// Step(-1) copies every page under a single read snapshot: writers keep
// going, and the copy is the database as of one commit.
func (t *Tenant) backup(ctx context.Context, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("tenants: backup %s: %w", dst, os.ErrExist)
	}
	conn, err := t.r.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	err = conn.Raw(func(driverConn any) error {
		b, err := driverConn.(interface {
			NewBackup(string) (*sqlite.Backup, error)
		}).NewBackup((&url.URL{Scheme: "file", Path: dst}).String())
		if err != nil {
			return err
		}
		_, err = b.Step(-1)
		return errors.Join(err, b.Finish())
	})
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
