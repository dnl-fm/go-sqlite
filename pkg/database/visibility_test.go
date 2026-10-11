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
	"testing"
	"time"
)

func TestMemorySharedVisibility(t *testing.T) {
	for _, path := range []string{":memory:", "file::memory:", "file::memory:?cache=shared", "file:shared?mode=memory&cache=shared"} {
		t.Run(path, func(t *testing.T) {
			ctx := context.Background()
			db, openErr := Open(ctx, path, WithConfig(DefaultConfig().WithMaxOpenConns(20)))
			if openErr != nil {
				t.Fatal(openErr)
			}
			defer db.Close()
			if db.Reader() != db.Writer() || db.DB() != db.Writer() || db.Writer().Stats().MaxOpenConnections != 1 {
				t.Fatal("memory must share one connection")
			}
			if _, err := db.Exec(ctx, "CREATE TABLE items(v INTEGER)"); err != nil {
				t.Fatal(err)
			}
			if err := db.Write(ctx, func(tx *sql.Tx) error { _, err := tx.Exec("INSERT INTO items VALUES(42)"); return err }); err != nil {
				t.Fatal(err)
			}
			var value int
			if err := db.QueryOne(ctx, "SELECT v FROM items").Scan(&value); err != nil || value != 42 {
				t.Fatalf("read value=%d err=%v", value, err)
			}
			rows, queryErr := db.Query(ctx, "SELECT v FROM items")
			if queryErr != nil {
				t.Fatal(queryErr)
			}
			if !rows.Next() {
				if err := rows.Err(); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				rows.Close()
				t.Fatal("missing shared row")
			}
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if err := db.Read(ctx, func(tx *sql.Tx) error { return tx.QueryRow("SELECT v FROM items").Scan(&value) }); err != nil || value != 42 {
				t.Fatalf("Read value=%d err=%v", value, err)
			}
		})
	}
}

func TestWriteSerializesConcurrentWriters(t *testing.T) {
	db := openStore(t, DefaultConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const workers, inserts = 16, 40
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := range workers {
		wg.Go(func() {
			for seq := range inserts {
				if err := db.Write(ctx, func(tx *sql.Tx) error { return insertItem(tx, worker, seq) }); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := countItems(t, db.Reader()); n != workers*inserts {
		t.Fatalf("count=%d want=%d", n, workers*inserts)
	}
}

func TestWriteReadModifyWriteHasNoLostUpdates(t *testing.T) {
	db := openStore(t, DefaultConfig())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	const workers, increments = 16, 40
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Go(func() {
			for range increments {
				if err := db.Write(ctx, func(tx *sql.Tx) error {
					var value int
					if err := tx.QueryRow("SELECT v FROM counter WHERE id=1").Scan(&value); err != nil {
						return err
					}
					_, err := tx.Exec("UPDATE counter SET v=? WHERE id=1", value+1)
					return err
				}); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	var value int
	if err := db.QueryOne(ctx, "SELECT v FROM counter WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	if value != workers*increments {
		t.Fatalf("counter=%d want=%d", value, workers*increments)
	}
}

func TestOpenRejectsLegacyDatabaseBeforeTouchingIt(t *testing.T) {
	for _, sidecar := range []bool{false, true} {
		t.Run(map[bool]string{false: "header", true: "sidecar"}[sidecar], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy.db")
			header := make([]byte, 100)
			copy(header, "SQLite format 3\x00")
			if !sidecar {
				header[18], header[19] = 0xff, 0xff
			}
			if err := os.WriteFile(path, header, 0o600); err != nil {
				t.Fatal(err)
			}
			if sidecar {
				if err := os.WriteFile(path+"-log", []byte("log"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Open(context.Background(), path)
			if err == nil || !strings.Contains(err.Error(), "cmd/turso-convert") {
				t.Fatalf("converter error=%v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, header) {
				t.Fatal("rejected database was modified")
			}
		})
	}
}

func TestCheckpointRestoresConfiguredBusyTimeout(t *testing.T) {
	db := openStore(t, DefaultConfig().WithPragma("busy_timeout", "1234"))
	ctx := context.Background()
	for _, expected := range []int{1234, 321} {
		if expected == 321 {
			if _, err := db.Exec(ctx, "PRAGMA busy_timeout=321"); err != nil {
				t.Fatal(err)
			}
		}
		if busy, err := db.Checkpoint(ctx, time.Millisecond); err != nil || busy {
			t.Fatalf("checkpoint busy=%t err=%v", busy, err)
		}
		var timeout int
		if err := db.Writer().QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != expected {
			t.Fatalf("timeout=%d want=%d", timeout, expected)
		}
	}
}

func TestBeginReadOnlyTxRoutesToReader(t *testing.T) {
	db := openStore(t, DefaultConfig())
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	assertReadOnly(t, insertItem(tx, 1, 1))
}

func TestWriteCancellationReturnsContextError(t *testing.T) {
	db := openStore(t, DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	err := db.Write(ctx, func(_ *sql.Tx) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v want canceled", err)
	}
}
