package database

import (
	"context"
	"strings"
	"testing"
)

// TestEveryConnectionGetsPragmas reserves every pooled connection at once, so
// each one is provably a distinct connection, and reads the pragmas back.
func TestEveryConnectionGetsPragmas(t *testing.T) {
	ctx := context.Background()
	store := openStore(t, DefaultConfig().WithMaxOpenConns(6).WithMaxIdleConns(6).WithPragma("synchronous", "FULL"))

	for i := range 6 {
		conn, err := store.Reader().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		for name, want := range map[string]string{"foreign_keys": "1", "busy_timeout": "5000", "journal_mode": "wal", "temp_store": "2", "synchronous": "2", "cache_size": "-16000"} {
			if got := pragma(t, conn, name); got != want {
				t.Errorf("reader conn %d: %s = %s, want %s", i, name, got, want)
			}
		}
	}
	if open := store.Reader().Stats().OpenConnections; open != 6 {
		t.Fatalf("reader pool has %d open connections, want 6", open)
	}

	conn, err := store.Writer().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	for name, want := range map[string]string{"foreign_keys": "1", "busy_timeout": "5000", "journal_mode": "wal", "synchronous": "2", "cache_size": "-16000"} {
		if got := pragma(t, conn, name); got != want {
			t.Errorf("writer conn: %s = %s, want %s", name, got, want)
		}
	}
}

func TestOpenStrictPragmaFailure(t *testing.T) {
	cfg := DefaultConfig().WithPragma("foreign_keys", ")").WithStrictPragmas(true)
	db, err := Open(context.Background(), t.TempDir()+"/strict.db", WithConfig(cfg))
	if db != nil {
		db.Close()
		t.Fatal("expected no database")
	}
	if err == nil || !strings.Contains(err.Error(), "PRAGMA foreign_keys = )") {
		t.Fatalf("strict pragma error: %v", err)
	}
}

func TestOpenNonStrictPragmaFailure(t *testing.T) {
	cfg := DefaultConfig().WithPragma("foreign_keys", ")")
	db, err := Open(context.Background(), t.TempDir()+"/lenient.db", WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Reader().Ping(); err != nil {
		t.Fatal(err)
	}
}
