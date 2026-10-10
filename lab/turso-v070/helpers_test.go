package tursov070

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func runChildProcess(dbPath string, processID, writes int, testName string) error {
	return runChildProcessWithEnv(dbPath, processID, writes, testName)
}

func runChildProcessWithEnv(dbPath string, processID, writes int, testName string, extraEnv ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+testName)
	cmd.Env = append(os.Environ(),
		"TURSO_V070_CHILD=1",
		"TURSO_V070_CONCURRENT_CHILD=1",
		"TURSO_V070_DB_PATH="+dbPath,
		"TURSO_V070_PROCESS_ID="+strconv.Itoa(processID),
		"TURSO_V070_WRITES="+strconv.Itoa(writes),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("child process %d timed out", processID)
	}
	if err != nil {
		return fmt.Errorf("child process %d failed: %w\n%s", processID, err, out)
	}
	return nil
}

func runMultiProcessChild(t *testing.T) {
	t.Helper()

	dbPath := os.Getenv("TURSO_V070_DB_PATH")
	processID, err := strconv.Atoi(os.Getenv("TURSO_V070_PROCESS_ID"))
	if err != nil {
		t.Fatalf("invalid process id: %v", err)
	}
	writes, err := strconv.Atoi(os.Getenv("TURSO_V070_WRITES"))
	if err != nil {
		t.Fatalf("invalid write count: %v", err)
	}

	dsn := os.Getenv("TURSO_V070_DSN")
	if dsn == "" {
		dsn = dbPath + "?_busy_timeout=5000"
	}
	db := openTurso(t, dsn)
	defer db.Close()

	for seq := range writes {
		if _, err := db.Exec(`insert into writes (process_id, seq) values (?, ?)`, processID, seq); err != nil {
			t.Fatalf("child %d seq %d failed: %v", processID, seq, err)
		}
	}
}

func multiprocessWALDSN(path string) string {
	return path + "?experimental=multiprocess_wal&_busy_timeout=5000"
}

func runLiveHolder(t *testing.T) {
	t.Helper()

	db := openTurso(t, os.Getenv("TURSO_V070_DSN"))
	defer db.Close()

	if _, err := db.Exec(`create table if not exists writes (id integer primary key, source text not null)`); err != nil {
		t.Fatalf("holder failed to create table: %v", err)
	}
	if _, err := db.Exec(`insert into writes(source) values('go-holder')`); err != nil {
		t.Fatalf("holder failed to insert row: %v", err)
	}
	if err := os.WriteFile(os.Getenv("TURSO_V070_READY"), []byte("ready"), 0o644); err != nil {
		t.Fatalf("holder failed to write ready file: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		if err := db.QueryRow(`select count(*) from writes`).Scan(&count); err != nil {
			t.Fatalf("holder failed to count rows: %v", err)
		}
		if count >= 2 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("holder did not observe tursodb write before timeout")
}

func waitForFile(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func openTurso(t *testing.T, dsn string) *sql.DB {
	t.Helper()
	db, err := sql.Open("turso", dsn)
	if err != nil {
		t.Fatalf("failed to open Turso db: %v", err)
	}
	return db
}
