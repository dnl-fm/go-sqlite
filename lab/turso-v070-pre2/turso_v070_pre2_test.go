package tursov070pre2

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "turso.tech/database/tursogo"
)

func TestWithoutRowIDRequiresExperimentalFlag(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "plain.db"))
	defer db.Close()

	_, err := db.Exec(`create table lookup (id text primary key, value text not null) without rowid`)
	if err == nil {
		t.Fatal("WITHOUT ROWID should still be gated without the experimental flag")
	}
	if !strings.Contains(err.Error(), "experimental") || !strings.Contains(err.Error(), "WITHOUT ROWID") {
		t.Fatalf("expected experimental WITHOUT ROWID error, got %v", err)
	}
}

func TestPlainTursoWithoutRowIDExperimentalFlag(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "plain.db")+"?experimental=without_rowid")
	defer db.Close()

	if _, err := db.Exec(`create table lookup (id text primary key, value text not null) without rowid`); err != nil {
		t.Fatalf("plain Turso should create experimental WITHOUT ROWID table: %v", err)
	}
	if _, err := db.Exec(`insert into lookup (id, value) values (?, ?)`, "a", "A"); err != nil {
		t.Fatalf("plain Turso should write experimental WITHOUT ROWID table: %v", err)
	}

	var got string
	if err := db.QueryRow(`select value from lookup where id = ?`, "a").Scan(&got); err != nil {
		t.Fatalf("plain Turso should read experimental WITHOUT ROWID table: %v", err)
	}
	if got != "A" {
		t.Fatalf("expected A, got %q", got)
	}
}

func TestMVCCStillRejectsWithoutRowIDWrites(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "mvcc.db")+"?experimental=without_rowid")
	defer db.Close()

	if _, err := db.Exec(`pragma journal_mode='mvcc'`); err != nil {
		t.Fatalf("failed to enable MVCC: %v", err)
	}
	if _, err := db.Exec(`create table lookup (id text primary key, value text not null) without rowid`); err != nil {
		t.Fatalf("MVCC currently creates the experimental WITHOUT ROWID table: %v", err)
	}

	_, err := db.Exec(`insert into lookup (id, value) values (?, ?)`, "a", "A")
	if err == nil {
		t.Fatal("MVCC should not promise writes to WITHOUT ROWID tables yet")
	}
	if !strings.Contains(err.Error(), "WITHOUT ROWID") || !strings.Contains(err.Error(), "MVCC") {
		t.Fatalf("expected MVCC WITHOUT ROWID rejection, got %v", err)
	}
}

func TestNativeFTSRequiresExperimentalIndexMethod(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "native-fts-gated.db"))
	defer db.Close()

	if _, err := db.Exec(`create table docs (id text primary key, body text not null)`); err != nil {
		t.Fatalf("failed to create docs table: %v", err)
	}
	_, err := db.Exec(`create index docs_fts on docs using fts (id, body) with (tokenizer = 'ngram')`)
	if err == nil {
		t.Fatal("native FTS should still be gated without experimental=index_method")
	}
	if !strings.Contains(err.Error(), "experimental") || !strings.Contains(err.Error(), "index method") {
		t.Fatalf("expected experimental index method error, got %v", err)
	}
}

func TestNativeFTSPlainGoDriverStillMissingModule(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "native-fts-plain.db")+"?experimental=index_method")
	defer db.Close()

	if _, err := db.Exec(`create table docs (id text primary key, body text not null)`); err != nil {
		t.Fatalf("failed to create docs table: %v", err)
	}
	_, err := db.Exec(`create index docs_fts on docs using fts (id, body) with (tokenizer = 'ngram')`)
	if err == nil {
		t.Fatal("native FTS should not be exposed by the Go driver yet")
	}
	if !strings.Contains(err.Error(), "unknown module name 'fts'") {
		t.Fatalf("expected missing fts module error, got %v", err)
	}
}

func TestNativeFTSStillUnsupportedInMVCC(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "native-fts-mvcc.db")+"?experimental=index_method")
	defer db.Close()

	if _, err := db.Exec(`pragma journal_mode='mvcc'`); err != nil {
		t.Fatalf("failed to enable MVCC: %v", err)
	}
	if _, err := db.Exec(`create table docs (id text primary key, body text not null)`); err != nil {
		t.Fatalf("failed to create docs table: %v", err)
	}
	_, err := db.Exec(`create index docs_fts on docs using fts (id, body) with (tokenizer = 'ngram')`)
	if err == nil {
		t.Fatal("native FTS should not be supported in MVCC yet")
	}
	if !strings.Contains(err.Error(), "Custom index modules are not supported in MVCC mode") {
		t.Fatalf("expected MVCC custom index module error, got %v", err)
	}
}

func TestSameProcessMVCCAutocommitWriteStorm(t *testing.T) {
	const writers = 32
	const writesPerWriter = 50

	db := openTurso(t, filepath.Join(t.TempDir(), "same-process-mvcc.db")+"?_busy_timeout=5000")
	defer db.Close()
	db.SetMaxOpenConns(writers)
	db.SetMaxIdleConns(writers)

	if _, err := db.Exec(`pragma journal_mode='mvcc'`); err != nil {
		t.Fatalf("failed to enable MVCC: %v", err)
	}
	if _, err := db.Exec(`create table writes (writer integer not null, seq integer not null, primary key (writer, seq))`); err != nil {
		t.Fatalf("failed to create writes table: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, writers*writesPerWriter)
	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			<-start
			for seq := range writesPerWriter {
				if _, err := db.Exec(`insert into writes (writer, seq) values (?, ?)`, writer, seq); err != nil {
					errs <- fmt.Errorf("writer %d seq %d: %w", writer, seq, err)
				}
			}
		}(writer)
	}

	close(start)
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		t.Fatalf("expected same-process Turso MVCC autocommit writes to succeed, got %d failures; first: %v", len(failures), failures[0])
	}

	var count int
	if err := db.QueryRow(`select count(*) from writes`).Scan(&count); err != nil {
		t.Fatalf("failed to count same-process writes: %v", err)
	}
	if want := writers * writesPerWriter; count != want {
		t.Fatalf("expected %d rows after same-process write storm, got %d", want, count)
	}
}

func TestSequentialMultiProcessAutocommitWrites(t *testing.T) {
	if os.Getenv("TURSO_V070_PRE2_CHILD") == "1" {
		runMultiProcessChild(t)
		return
	}

	const processes = 4
	const writesPerProcess = 25

	dbPath := filepath.Join(t.TempDir(), "multiprocess.db")
	db := openTurso(t, dbPath+"?_busy_timeout=5000")
	if _, err := db.Exec(`create table writes (process_id integer not null, seq integer not null, primary key (process_id, seq))`); err != nil {
		t.Fatalf("failed to create writes table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close setup db: %v", err)
	}

	for processID := range processes {
		if err := runChildProcess(dbPath, processID, writesPerProcess, "TestSequentialMultiProcessAutocommitWrites"); err != nil {
			t.Fatal(err)
		}
	}

	verify := openTurso(t, dbPath+"?_busy_timeout=5000")
	defer verify.Close()

	var count int
	if err := verify.QueryRow(`select count(*) from writes`).Scan(&count); err != nil {
		t.Fatalf("failed to count multiprocess writes: %v", err)
	}
	if want := processes * writesPerProcess; count != want {
		t.Fatalf("expected %d rows after multiprocess writes, got %d", want, count)
	}
}

func TestConcurrentMultiProcessWritersNeedExperimentalWAL(t *testing.T) {
	if os.Getenv("TURSO_V070_PRE2_CONCURRENT_CHILD") == "1" {
		runMultiProcessChild(t)
		return
	}

	const processes = 4
	const writesPerProcess = 25

	dbPath := filepath.Join(t.TempDir(), "multiprocess-concurrent.db")
	db := openTurso(t, dbPath+"?_busy_timeout=5000")
	if _, err := db.Exec(`create table writes (process_id integer not null, seq integer not null, primary key (process_id, seq))`); err != nil {
		t.Fatalf("failed to create writes table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close setup db: %v", err)
	}

	errs := make(chan error, processes)
	for processID := range processes {
		go func() {
			errs <- runChildProcess(dbPath, processID, writesPerProcess, "TestConcurrentMultiProcessWritersNeedExperimentalWAL")
		}()
	}

	var failures []error
	for range processes {
		if err := <-errs; err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		t.Fatal("expected overlapping child writers to expose Turso's current file-lock boundary")
	}

	first := failures[0].Error()
	if !strings.Contains(first, "File is locked by another process") {
		t.Fatalf("expected file-locking failure, got %v", failures[0])
	}
}

func TestExperimentalMultiProcessWALDoesNotYetUnlockConcurrentGoProcesses(t *testing.T) {
	if os.Getenv("TURSO_V070_PRE2_MULTIPROCESS_WAL_CHILD") == "1" {
		runMultiProcessChild(t)
		return
	}

	const processes = 4
	const writesPerProcess = 25

	dbPath := filepath.Join(t.TempDir(), "multiprocess-wal.db")
	db := openTurso(t, multiprocessWALDSN(dbPath))
	if _, err := db.Exec(`create table writes (process_id integer not null, seq integer not null, primary key (process_id, seq))`); err != nil {
		t.Fatalf("failed to create writes table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close setup db: %v", err)
	}

	errs := make(chan error, processes)
	for processID := range processes {
		go func() {
			errs <- runChildProcessWithEnv(
				dbPath,
				processID,
				writesPerProcess,
				"TestExperimentalMultiProcessWALDoesNotYetUnlockConcurrentGoProcesses",
				"TURSO_V070_PRE2_MULTIPROCESS_WAL_CHILD=1",
				"TURSO_V070_PRE2_DSN="+multiprocessWALDSN(dbPath),
			)
		}()
	}

	var failures []error
	for range processes {
		if err := <-errs; err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) == 0 {
		t.Fatal("expected concurrent Go child writers to still expose a WAL file-locking boundary")
	}
	if !strings.Contains(failures[0].Error(), "File is locked by another process") {
		t.Fatalf("expected WAL file-locking failure, got %v", failures[0])
	}
}

func TestExperimentalMultiProcessWALAllowsTursoDBCLIToInspectLiveGoApp(t *testing.T) {
	tursodb := os.Getenv("TURSO_V070_PRE2_TURSODB_BIN")
	if tursodb == "" {
		t.Skip("set TURSO_V070_PRE2_TURSODB_BIN to a Turso 0.7.0-pre.2 tursodb binary to run the CLI live-inspection probe")
	}
	if os.Getenv("TURSO_V070_PRE2_HOLDER") == "1" {
		runLiveHolder(t)
		return
	}

	dbPath := filepath.Join(t.TempDir(), "live-app.db")
	readyPath := dbPath + ".ready"

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	holder := exec.CommandContext(ctx, os.Args[0], "-test.run=TestExperimentalMultiProcessWALAllowsTursoDBCLIToInspectLiveGoApp")
	holder.Env = append(os.Environ(),
		"TURSO_V070_PRE2_HOLDER=1",
		"TURSO_V070_PRE2_DSN="+multiprocessWALDSN(dbPath),
		"TURSO_V070_PRE2_READY="+readyPath,
	)
	holderOut, err := holder.StderrPipe()
	if err != nil {
		t.Fatalf("failed to capture holder stderr: %v", err)
	}
	if err := holder.Start(); err != nil {
		t.Fatalf("failed to start holder: %v", err)
	}

	waitForFile(t, readyPath)

	read := exec.CommandContext(ctx, tursodb, "--experimental-multiprocess-wal", "-q", dbPath, "select count(*) from writes;")
	readOut, err := read.CombinedOutput()
	if err != nil {
		t.Fatalf("tursodb should inspect live Go app database: %v\n%s", err, readOut)
	}
	if !strings.Contains(string(readOut), "1") {
		t.Fatalf("expected tursodb read to see holder row, got:\n%s", readOut)
	}

	write := exec.CommandContext(ctx, tursodb, "--experimental-multiprocess-wal", "-q", dbPath, "insert into writes(source) values('tursodb');")
	writeOut, err := write.CombinedOutput()
	if err != nil {
		t.Fatalf("tursodb should write to live Go app database: %v\n%s", err, writeOut)
	}

	if err := holder.Wait(); err != nil {
		holderErr, _ := io.ReadAll(holderOut)
		t.Fatalf("holder failed: %v\n%s", err, holderErr)
	}

	verify := exec.CommandContext(ctx, tursodb, "--experimental-multiprocess-wal", "-q", dbPath, "select source, count(*) from writes group by source order by source;")
	verifyOut, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to verify CLI write: %v\n%s", err, verifyOut)
	}
	got := string(verifyOut)
	if !strings.Contains(got, "go-holder") || !strings.Contains(got, "tursodb") {
		t.Fatalf("expected both holder and tursodb rows, got:\n%s", got)
	}
}

func runChildProcess(dbPath string, processID, writes int, testName string) error {
	return runChildProcessWithEnv(dbPath, processID, writes, testName)
}

func runChildProcessWithEnv(dbPath string, processID, writes int, testName string, extraEnv ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+testName)
	cmd.Env = append(os.Environ(),
		"TURSO_V070_PRE2_CHILD=1",
		"TURSO_V070_PRE2_CONCURRENT_CHILD=1",
		"TURSO_V070_PRE2_DB_PATH="+dbPath,
		"TURSO_V070_PRE2_PROCESS_ID="+strconv.Itoa(processID),
		"TURSO_V070_PRE2_WRITES="+strconv.Itoa(writes),
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

	dbPath := os.Getenv("TURSO_V070_PRE2_DB_PATH")
	processID, err := strconv.Atoi(os.Getenv("TURSO_V070_PRE2_PROCESS_ID"))
	if err != nil {
		t.Fatalf("invalid process id: %v", err)
	}
	writes, err := strconv.Atoi(os.Getenv("TURSO_V070_PRE2_WRITES"))
	if err != nil {
		t.Fatalf("invalid write count: %v", err)
	}

	dsn := os.Getenv("TURSO_V070_PRE2_DSN")
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

	db := openTurso(t, os.Getenv("TURSO_V070_PRE2_DSN"))
	defer db.Close()

	if _, err := db.Exec(`create table if not exists writes (id integer primary key, source text not null)`); err != nil {
		t.Fatalf("holder failed to create table: %v", err)
	}
	if _, err := db.Exec(`insert into writes(source) values('go-holder')`); err != nil {
		t.Fatalf("holder failed to insert row: %v", err)
	}
	if err := os.WriteFile(os.Getenv("TURSO_V070_PRE2_READY"), []byte("ready"), 0o644); err != nil {
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
