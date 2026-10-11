package database

import (
	"context"
	"database/sql"
	"errors"
	"modernc.org/sqlite"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const writerSchema = `CREATE TABLE items (id INTEGER PRIMARY KEY, worker INTEGER NOT NULL, seq INTEGER NOT NULL, payload TEXT NOT NULL, UNIQUE(worker, seq)); CREATE TABLE counter (id INTEGER PRIMARY KEY, v INTEGER NOT NULL); INSERT INTO counter VALUES(1, 0);`

func openStore(t *testing.T, cfg *Config) *Database {
	t.Helper()
	d, err := Open(context.Background(), filepath.Join(t.TempDir(), "writer.db"), WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(context.Background(), writerSchema); err != nil {
		t.Fatal(err)
	}
	return d
}

func insertItem(tx *sql.Tx, worker, seq int) error {
	_, err := tx.Exec("INSERT INTO items (worker, seq, payload) VALUES (?, ?, 'payload')", worker, seq)
	return err
}

func countItems(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	err := db.QueryRow("SELECT count(*) FROM items").Scan(&n)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func pragma(t *testing.T, conn *sql.Conn, name string) string {
	t.Helper()
	var v string
	err := conn.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v)
	if err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	return v
}

func TestDefaults(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())
	if stats := store.Reader().Stats(); stats.MaxOpenConnections != 4 {
		t.Errorf("reader pool max = %d, want 4", stats.MaxOpenConnections)
	}
	if stats := store.Writer().Stats(); stats.MaxOpenConnections != 1 {
		t.Errorf("writer pool max = %d, want 1", stats.MaxOpenConnections)
	}
	conn, err := store.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if got := pragma(t, conn, "synchronous"); got != "1" {
		t.Errorf("synchronous = %s, want 1 (NORMAL)", got)
	}
}

func TestReaderIsReadOnly(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())

	_, err := store.Reader().Exec("INSERT INTO items (worker, seq, payload) VALUES (1, 1, 'x')")
	assertReadOnly(t, err)
	err = store.Read(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, 2) })
	assertReadOnly(t, err)
	// mode=ro is not a setting a statement can undo.
	_, _ = store.Reader().Exec("PRAGMA query_only = 0")
	_, err = store.Reader().Exec("INSERT INTO items (worker, seq, payload) VALUES (1, 3, 'x')")
	assertReadOnly(t, err)

	if n := countItems(t, store.Reader()); n != 0 {
		t.Fatalf("rows = %d, want 0", n)
	}
}

func assertReadOnly(t *testing.T, err error) {
	t.Helper()
	var se *sqlite.Error
	if !errors.As(err, &se) || se.Code()&0xff != 8 { // SQLITE_READONLY
		t.Fatalf("write through Reader: got %v, want SQLITE_READONLY", err)
	}
	t.Logf("write through Reader fails: %v", err)
}

func TestWrite(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())

	err := store.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, 1) })
	if err != nil {
		t.Fatal(err)
	}

	boom := errors.New("boom")
	err = store.Write(ctx, func(tx *sql.Tx) error {
		if insertErr := insertItem(tx, 1, 2); insertErr != nil {
			return insertErr
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want boom", err)
	}

	func() {
		defer func() {
			if p := recover(); p != "kaboom" {
				t.Fatalf("recovered %v, want the original panic", p)
			}
		}()
		_ = store.Write(ctx, func(tx *sql.Tx) error {
			if insertErr := insertItem(tx, 1, 3); insertErr != nil {
				return insertErr
			}
			panic("kaboom")
		})
	}()

	// The writer connection is free again (a leaked tx would deadlock here)
	// and only the committed row exists.
	err = store.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, 4) })
	if err != nil {
		t.Fatal(err)
	}
	if n := countItems(t, store.Reader()); n != 2 {
		t.Fatalf("rows = %d, want 2", n)
	}
}

// TestReadIsASnapshot proves a read transaction neither blocks the writer nor
// sees its commits.
func TestReadIsASnapshot(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())

	err := store.Read(ctx, func(tx *sql.Tx) error {
		var before, after int
		if err := tx.QueryRow("SELECT count(*) FROM items").Scan(&before); err != nil {
			return err
		}
		if err := store.Write(ctx, func(w *sql.Tx) error { return insertItem(w, 1, 1) }); err != nil {
			return err
		}
		if err := tx.QueryRow("SELECT count(*) FROM items").Scan(&after); err != nil {
			return err
		}
		if before != 0 || after != 0 {
			t.Errorf("snapshot saw %d then %d rows, want 0 and 0", before, after)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countItems(t, store.Reader()); n != 1 {
		t.Fatalf("rows after read tx = %d, want 1", n)
	}
}

// TestCheckpointNeedsTheWriter pins a consequence of mode=ro: maintenance
// goes through Writer. A read-only connection cannot checkpoint.
func TestCheckpointNeedsTheWriter(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())
	for seq := range 50 {
		writeErr := store.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, seq) })
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	}

	var busy, log, done int
	err := store.Reader().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &done)
	if err == nil {
		t.Fatalf("checkpoint through Reader succeeded (busy=%d log=%d done=%d)", busy, log, done)
	}
	t.Logf("checkpoint through Reader fails: %v", err)

	err = store.Writer().QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &done)
	if err != nil || busy != 0 {
		t.Fatalf("checkpoint through Writer: busy=%d err=%v", busy, err)
	}
}

// TestCheckpointQuietWindow pins what Store.Checkpoint promises: it empties
// the WAL, and a read transaction it cannot wait out costs the writers the
// quiet window, not busy_timeout.
func TestCheckpointQuietWindow(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig())
	var (
		seq        int
		name, file string
	)
	err := store.Writer().QueryRow("PRAGMA database_list").Scan(&seq, &name, &file)
	if err != nil {
		t.Fatal(err)
	}
	write := func(seq int) {
		t.Helper()
		writeErr := store.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, seq) })
		if writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	for seq := range 50 {
		write(seq)
	}

	busy, err := store.Checkpoint(ctx, 50*time.Millisecond)
	if err != nil || busy {
		t.Fatalf("checkpoint with no reader: busy=%t err=%v", busy, err)
	}
	fi, err := os.Stat(file + "-wal")
	if err != nil || fi.Size() != 0 {
		t.Fatalf("wal after the checkpoint: %v, err=%v; want an empty file", fi, err)
	}

	// A read transaction on Reader is not behind the window, and this one
	// never ends.
	held, err := store.Reader().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()
	var n int
	_, err = held.ExecContext(ctx, "BEGIN")
	if err == nil {
		err = held.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&n)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = held.ExecContext(ctx, "ROLLBACK") }()
	write(50)

	began := time.Now()
	busy, err = store.Checkpoint(ctx, 50*time.Millisecond)
	if err != nil || !busy {
		t.Fatalf("checkpoint behind an open read transaction: busy=%t err=%v, want busy", busy, err)
	}
	if took := time.Since(began); took > time.Second {
		t.Errorf("checkpoint behind an open read transaction took %v, want about the 50ms window, not the 5s busy_timeout", took)
	}
	write(51) // the writer connection is free again
	conn, err := store.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if got := pragma(t, conn, "busy_timeout"); got != "5000" {
		t.Errorf("writer busy_timeout after Checkpoint = %s, want 5000", got)
	}
}
