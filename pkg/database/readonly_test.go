package database

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func openReadOnly(t *testing.T, path string, cfg *Config) *Database {
	t.Helper()
	d, err := OpenReadOnly(context.Background(), path, WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenReadOnlySeesOwnerWrites(t *testing.T) {
	ctx := context.Background()
	owner := openStore(t, DefaultConfig())
	ro := openReadOnly(t, owner.Path(), DefaultConfig())
	if stats := ro.Reader().Stats(); stats.MaxOpenConnections != 4 {
		t.Errorf("reader pool max = %d, want 4", stats.MaxOpenConnections)
	}

	for want := 1; want <= 2; want++ {
		if err := owner.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, want) }); err != nil {
			t.Fatal(err)
		}
		var viaQueryOne, viaQuery, viaRead, viaTx int
		if err := ro.QueryOne(ctx, "SELECT count(*) FROM items").Scan(&viaQueryOne); err != nil {
			t.Fatal(err)
		}
		rows, err := ro.Query(ctx, "SELECT seq FROM items")
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			viaQuery++
		}
		if err = errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if err = ro.Read(ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT count(*) FROM items").Scan(&viaRead) }); err != nil {
			t.Fatal(err)
		}
		tx, err := ro.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		if err = errors.Join(tx.QueryRow("SELECT count(*) FROM items").Scan(&viaTx), tx.Rollback()); err != nil {
			t.Fatal(err)
		}
		for name, got := range map[string]int{"QueryOne": viaQueryOne, "Query": viaQuery, "Read": viaRead, "BeginTx": viaTx, "Reader": countItems(t, ro.Reader())} {
			if got != want {
				t.Errorf("%s sees %d rows, want %d", name, got, want)
			}
		}
	}
}

func TestOpenReadOnlyRejectsEveryWritePath(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "owned.db")
	owner, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Exec(ctx, writerSchema); err != nil {
		t.Fatal(err)
	}
	// Closing the owner checkpoints the WAL: the file now holds every byte.
	if err = owner.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	fn := func(tx *sql.Tx) error { called = true; return insertItem(tx, 1, 1) }
	queue := NewQueue(ro, QueueOptions{})
	_, execErr := ro.Exec(ctx, "INSERT INTO items (worker, seq, payload) VALUES (1, 1, 'p')")
	_, beginErr := ro.BeginTx(ctx, nil)
	_, beginOptsErr := ro.BeginTx(ctx, &sql.TxOptions{})
	_, checkpointErr := ro.Checkpoint(ctx, time.Millisecond)
	for name, got := range map[string]error{
		"Exec": execErr, "Write": ro.Write(ctx, fn), "BeginTx(nil)": beginErr, "BeginTx(read-write)": beginOptsErr,
		"Checkpoint": checkpointErr, "Queue.Submit": queue.Submit(ctx, fn),
	} {
		if !errors.Is(got, ErrReadOnly) {
			t.Errorf("%s: got %v, want ErrReadOnly", name, got)
		}
	}
	if called {
		t.Error("a write callback ran on a read-only database")
	}
	if err = queue.Close(); err != nil {
		t.Fatal(err)
	}

	// The raw handles cannot return ErrReadOnly; SQLite's mode=ro rejects the write.
	for name, pool := range map[string]*sql.DB{"DB": ro.DB(), "Writer": ro.Writer(), "Reader": ro.Reader()} {
		if pool == nil {
			t.Fatalf("%s() is nil", name)
		}
		_, err = pool.Exec("INSERT INTO items (worker, seq, payload) VALUES (1, 1, 'p')")
		assertReadOnly(t, err)
		tx, beginErr := pool.Begin()
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		assertReadOnly(t, insertItem(tx, 1, 1))
		if err = tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
	// query_only is not what protects the file: turning it off changes nothing.
	conn, err := ro.Reader().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, "PRAGMA query_only = OFF"); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, "DELETE FROM counter")
	assertReadOnly(t, err)
	if err = conn.Close(); err != nil {
		t.Fatal(err)
	}

	if n := countItems(t, ro.Reader()); n != 0 {
		t.Errorf("items = %d, want 0", n)
	}
	if err = ro.Close(); err != nil {
		t.Fatal(err)
	}
	assertFileUnchanged(t, path, before)
}

// assertFileUnchanged checks the database bytes and that no WAL frame exists.
func assertFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Error("database file was modified")
	}
	if info, err := os.Stat(path + "-wal"); err == nil && info.Size() != 0 {
		t.Errorf("WAL has %d bytes, want none", info.Size())
	}
}

func TestOpenReadOnlyMissingFile(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenReadOnly(context.Background(), filepath.Join(dir, "missing.db"))
	if db != nil {
		db.Close()
		t.Fatal("expected no database")
	}
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "owner creates it") {
		t.Fatalf("missing file error: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("directory has %d entries (err=%v), want none", len(entries), err)
	}
}

func TestOpenReadOnlyRejectsLegacyDatabaseBeforeTouchingIt(t *testing.T) {
	for _, sidecar := range []bool{false, true} {
		t.Run(map[bool]string{false: "header", true: "sidecar"}[sidecar], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "legacy.db")
			header := make([]byte, 100)
			copy(header, "SQLite format 3\x00")
			want := 1
			if sidecar {
				want = 2
				if err := os.WriteFile(path+"-log", []byte("log"), 0o600); err != nil {
					t.Fatal(err)
				}
			} else {
				header[18], header[19] = 0xff, 0xff
			}
			if err := os.WriteFile(path, header, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := OpenReadOnly(context.Background(), path)
			if err == nil || !strings.Contains(err.Error(), "cmd/turso-convert") {
				t.Fatalf("converter error=%v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, header) {
				t.Fatal("rejected database was modified")
			}
			// SQLite would have left a -wal or -shm file behind.
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != want {
				t.Fatalf("directory has %d entries (err=%v), want %d", len(entries), err, want)
			}
		})
	}
}

func TestOpenReadOnlyRejectsMemoryPaths(t *testing.T) {
	for _, path := range []string{":memory:", "file::memory:", "file::memory:?cache=shared", "file:shared?mode=memory&cache=shared"} {
		db, err := OpenReadOnly(context.Background(), path)
		if db != nil {
			db.Close()
			t.Fatalf("%s: expected no database", path)
		}
		if !errors.Is(err, ErrInvalidPath) || !strings.Contains(err.Error(), "in-memory") {
			t.Errorf("%s: got %v, want an in-memory ErrInvalidPath", path, err)
		}
	}
	if _, err := OpenReadOnly(context.Background(), ""); !errors.Is(err, ErrInvalidPath) {
		t.Errorf("empty path: got %v, want ErrInvalidPath", err)
	}
}

func TestOpenReadOnlyRequiresWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, execErr := raw.Exec("CREATE TABLE items(v INTEGER)")
	if err = errors.Join(execErr, raw.Close()); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenReadOnly(context.Background(), path)
	if db != nil {
		db.Close()
		t.Fatal("expected no database")
	}
	if err == nil || !strings.Contains(err.Error(), `journal_mode "delete", want WAL`) || !strings.Contains(err.Error(), "block the owner") {
		t.Fatalf("journal mode error: %v", err)
	}
	assertFileUnchanged(t, path, before)
}

// TestOpenReadOnlyEveryConnectionGetsPragmas reserves every pooled connection
// at once, so each one is provably a distinct connection.
func TestOpenReadOnlyEveryConnectionGetsPragmas(t *testing.T) {
	ctx := context.Background()
	owner := openStore(t, DefaultConfig())
	cfg := DefaultConfig().WithMaxOpenConns(6).WithMaxIdleConns(6).
		WithPragma("busy_timeout", "1234").WithPragma("query_only", "ON").WithPragma("synchronous", "OFF")
	ro := openReadOnly(t, owner.Path(), cfg)

	// synchronous is the owner's setting: OFF (0) must not reach the connection.
	want := map[string]string{"busy_timeout": "1234", "query_only": "1", "foreign_keys": "1", "temp_store": "2", "cache_size": "-16000", "journal_mode": "wal", "synchronous": "2"}
	for i := range 6 {
		conn, err := ro.Reader().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		for name, value := range want {
			if got := pragma(t, conn, name); got != value {
				t.Errorf("conn %d: %s = %s, want %s", i, name, got, value)
			}
		}
	}
	if open := ro.Reader().Stats().OpenConnections; open != 6 {
		t.Fatalf("reader pool has %d open connections, want 6", open)
	}
}

func TestOpenReadOnlyOmitsPragmasThatWrite(t *testing.T) {
	ctx := context.Background()
	owner := openStore(t, DefaultConfig())
	cfg := DefaultConfig().WithPragma("user_version", "7")

	ro := openReadOnly(t, owner.Path(), cfg)
	var version int
	if err := ro.QueryOne(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 0 {
		t.Fatalf("user_version=%d err=%v, want 0", version, err)
	}

	db, err := OpenReadOnly(ctx, owner.Path(), WithConfig(cfg.WithStrictPragmas(true)))
	if db != nil {
		db.Close()
		t.Fatal("expected no database")
	}
	if err == nil || !strings.Contains(err.Error(), "PRAGMA user_version = 7") {
		t.Fatalf("strict pragma error: %v", err)
	}
}

func TestOpenReadOnlyConcurrentWithOwner(t *testing.T) {
	ctx := context.Background()
	owner := openStore(t, DefaultConfig())
	deadline := time.Now().Add(300 * time.Millisecond)

	var written atomic.Int64
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	wg.Go(func() {
		for seq := 0; time.Now().Before(deadline); seq++ {
			if err := owner.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, 1, seq) }); err != nil {
				errs <- err
				return
			}
			written.Add(1)
		}
	})
	reads := make([]int, 4)
	for i := range reads {
		ro := openReadOnly(t, owner.Path(), DefaultConfig())
		wg.Go(func() {
			last := 0
			for time.Now().Before(deadline) {
				var n int
				if err := ro.QueryOne(ctx, "SELECT count(*) FROM items").Scan(&n); err != nil {
					errs <- err
					return
				}
				if n < last {
					errs <- errors.New("count went backwards")
					return
				}
				last = n
				reads[i]++
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	total := int(written.Load())
	if total == 0 {
		t.Fatal("owner wrote nothing")
	}
	for i, n := range reads {
		if n == 0 {
			t.Errorf("read-only handle %d read nothing", i)
		}
	}
	ro := openReadOnly(t, owner.Path(), DefaultConfig())
	if n := countItems(t, ro.Reader()); n != total {
		t.Fatalf("read-only count=%d, owner wrote %d", n, total)
	}
	t.Logf("owner wrote %d rows; read-only handles read %v times", total, reads)
}
