package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dnl-fm/go-sqlite/pkg/driver/turso"
)

func TestTursoDriverOpen(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/open.db", WithDriver(turso.DriverName))
	if err != nil {
		t.Fatalf("failed to open turso database: %v", err)
	}
	defer db.Close()

	if db.Config().Driver != turso.DriverName {
		t.Fatalf("expected driver %q, got %q", turso.DriverName, db.Config().Driver)
	}

	if _, err := db.Exec(ctx, "CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}
	if _, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Ada"); err != nil {
		t.Fatalf("failed to insert row: %v", err)
	}

	var name string
	if err := db.QueryOne(ctx, "SELECT name FROM users WHERE id = ?", 1).Scan(&name); err != nil {
		t.Fatalf("failed to query row: %v", err)
	}
	if name != "Ada" {
		t.Fatalf("expected Ada, got %q", name)
	}
}

func TestTursoConcurrentAutocommitWrites(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/autocommit.db", WithConfig(tursoBattleConfig()))
	if err != nil {
		t.Fatalf("failed to open turso database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(ctx, "CREATE TABLE hits (id INTEGER PRIMARY KEY, worker INTEGER NOT NULL, seq INTEGER NOT NULL)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	const workers = 16
	const writesPerWorker = 50

	var wg sync.WaitGroup
	errs := make(chan error, workers*writesPerWorker)
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for seq := range writesPerWorker {
				if _, err := db.Exec(ctx, "INSERT INTO hits (worker, seq) VALUES (?, ?)", worker, seq); err != nil {
					errs <- fmt.Errorf("worker %d seq %d: %w", worker, seq, err)
				}
			}
		}(worker)
	}
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		t.Fatalf("expected concurrent autocommit writes to succeed, got %d failures; first: %v", len(failures), failures[0])
	}

	var count int
	if err := db.QueryOne(ctx, "SELECT COUNT(*) FROM hits").Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if want := workers * writesPerWorker; count != want {
		t.Fatalf("expected %d rows, got %d", want, count)
	}
}

func TestTursoMVCCBeginConcurrentDisjointWrites(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/mvcc.db", WithTursoMVCC())
	if err != nil {
		t.Fatalf("failed to open turso database: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(ctx, "CREATE TABLE hits (id INTEGER PRIMARY KEY, worker INTEGER NOT NULL UNIQUE)"); err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	const workers = 16
	start := make(chan struct{})
	commit := make(chan struct{})
	errs := make(chan error, workers)

	var wg sync.WaitGroup
	for worker := range workers {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			if err := insertWithBeginConcurrent(ctx, db, worker, start, commit); err != nil {
				errs <- err
			}
		}(worker)
	}

	time.Sleep(50 * time.Millisecond)
	close(start)
	time.Sleep(100 * time.Millisecond)
	close(commit)
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		t.Fatalf("expected disjoint BEGIN CONCURRENT writes to succeed, got %d failures; first: %v", len(failures), failures[0])
	}

	var count int
	if err := db.QueryOne(ctx, "SELECT COUNT(*) FROM hits").Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if count != workers {
		t.Fatalf("expected %d rows, got %d", workers, count)
	}
}

func TestTursoMVCCConcurrentWritesAcrossDatabaseHandles(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/shared.db"

	setupDB, openErr := Open(ctx, path, WithTursoMVCC())
	if openErr != nil {
		t.Fatalf("failed to open setup database: %v", openErr)
	}
	if _, execErr := setupDB.Exec(ctx, "CREATE TABLE hits (id INTEGER PRIMARY KEY, worker INTEGER NOT NULL UNIQUE)"); execErr != nil {
		t.Fatalf("failed to create table: %v", execErr)
	}
	if closeErr := setupDB.Close(); closeErr != nil {
		t.Fatalf("failed to close setup database: %v", closeErr)
	}

	const handles = 4
	const workersPerHandle = 8
	dbs := make([]*Database, 0, handles)
	for i := range handles {
		dbHandle, handleErr := Open(ctx, path, WithTursoMVCC())
		if handleErr != nil {
			t.Fatalf("failed to open database handle %d: %v", i, handleErr)
		}
		dbs = append(dbs, dbHandle)
	}
	defer func() {
		for _, dbHandle := range dbs {
			if closeErr := dbHandle.Close(); closeErr != nil {
				t.Errorf("failed to close database handle: %v", closeErr)
			}
		}
	}()

	start := make(chan struct{})
	commit := make(chan struct{})
	errs := make(chan error, handles*workersPerHandle)

	var wg sync.WaitGroup
	for handle, dbHandle := range dbs {
		for worker := range workersPerHandle {
			id := handle*workersPerHandle + worker
			wg.Add(1)
			go func(db *Database, id int) {
				defer wg.Done()
				if insertErr := insertWithBeginConcurrent(ctx, db, id, start, commit); insertErr != nil {
					errs <- insertErr
				}
			}(dbHandle, id)
		}
	}

	time.Sleep(50 * time.Millisecond)
	close(start)
	time.Sleep(100 * time.Millisecond)
	close(commit)
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		t.Fatalf("expected cross-handle BEGIN CONCURRENT writes to succeed, got %d failures; first: %v", len(failures), failures[0])
	}

	verify, err := Open(ctx, path, WithTursoMVCC())
	if err != nil {
		t.Fatalf("failed to open verify database: %v", err)
	}
	defer verify.Close()

	var count int
	if err := verify.QueryOne(ctx, "SELECT COUNT(*) FROM hits").Scan(&count); err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if want := handles * workersPerHandle; count != want {
		t.Fatalf("expected %d rows, got %d", want, count)
	}
}

func TestTursoBeginConcurrentRequiresMVCC(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/requires-mvcc.db", WithConfig(tursoBattleConfig()))
	if err != nil {
		t.Fatalf("failed to open turso database: %v", err)
	}
	defer db.Close()

	conn, err := db.DB().Conn(ctx)
	if err != nil {
		t.Fatalf("failed to reserve connection: %v", err)
	}
	defer conn.Close()

	_, err = conn.ExecContext(ctx, "BEGIN CONCURRENT")
	if err == nil {
		t.Fatal("expected BEGIN CONCURRENT to fail before MVCC journal mode is enabled")
	}
}

func tursoBattleConfig() *Config {
	return DefaultConfig().
		WithDriver(turso.DriverName).
		WithMaxOpenConns(32).
		WithMaxIdleConns(16).
		WithPragma("busy_timeout", "1000")
}

func insertWithBeginConcurrent(ctx context.Context, db *Database, worker int, start, commit <-chan struct{}) error {
	<-start
	return db.ConcurrentTx(ctx, func(tx ConnTx) error {
		if _, err := tx.ExecContext(ctx, "INSERT INTO hits (worker) VALUES (?)", worker); err != nil {
			return fmt.Errorf("worker %d insert: %w", worker, err)
		}
		<-commit
		return nil
	})
}
