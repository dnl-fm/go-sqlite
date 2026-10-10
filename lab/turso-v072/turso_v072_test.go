package tursov072

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "turso.tech/database/tursogo"
)

func TestFailedUpsertPreservesTableAndIndexes(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "upsert-integrity.db"))
	defer db.Close()

	if _, err := db.Exec(`
		create table accounts (
			id integer primary key,
			username text not null unique,
			email text not null unique,
			label text not null
		);
		insert into accounts (username, email, label) values
			('alice', 'alice@example.test', 'Alice'),
			('bob', 'bob@example.test', 'Bob');
	`); err != nil {
		t.Fatalf("failed to prepare accounts: %v", err)
	}

	_, err := db.Exec(`
		insert into accounts (username, email, label)
		values ('alice', 'bob@example.test', 'corrupted')
		on conflict(username) do update set
			email = excluded.email,
			label = excluded.label
	`)
	if err == nil {
		t.Fatal("expected the second UNIQUE constraint to reject the UPSERT")
	}

	var integrity string
	if err := db.QueryRow(`pragma integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("failed to run integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("expected intact database after failed UPSERT, got %q", integrity)
	}

	assertAccount := func(query, key, wantUsername, wantEmail, wantLabel string) {
		t.Helper()
		var username, email, label string
		if err := db.QueryRow(query, key).Scan(&username, &email, &label); err != nil {
			t.Fatalf("indexed account read failed: %v", err)
		}
		if username != wantUsername || email != wantEmail || label != wantLabel {
			t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)", username, email, label, wantUsername, wantEmail, wantLabel)
		}
	}
	assertAccount(
		`select username, email, label from accounts indexed by sqlite_autoindex_accounts_1 where username = ?`,
		"alice", "alice", "alice@example.test", "Alice",
	)
	assertAccount(
		`select username, email, label from accounts indexed by sqlite_autoindex_accounts_2 where email = ?`,
		"bob@example.test", "bob", "bob@example.test", "Bob",
	)
}

func TestLargeLiteralINUsesIndexedSeeks(t *testing.T) {
	db := openTurso(t, filepath.Join(t.TempDir(), "large-in-plan.db"))
	defer db.Close()

	if _, err := db.Exec(`
		create table records (id integer primary key, lookup_key text not null);
		create index records_lookup_key on records (lookup_key);
	`); err != nil {
		t.Fatalf("failed to prepare indexed table: %v", err)
	}

	literals := make([]string, 1_000)
	for i := range literals {
		literals[i] = fmt.Sprintf("'key-%04d'", i)
	}
	query := `explain query plan select id from records where lookup_key in (` + strings.Join(literals, ",") + `)`

	var details []string
	rows, err := db.Query(query)
	if err != nil {
		t.Fatalf("failed to explain large literal IN query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("failed to scan query plan: %v", err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed while reading query plan: %v", err)
	}

	plan := strings.Join(details, "\n")
	if !strings.Contains(plan, "SEARCH records USING INDEX records_lookup_key (lookup_key=?)") {
		t.Fatalf("expected indexed seeks for large literal IN query, got:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN records") || strings.Contains(plan, "SCAN records USING COVERING INDEX records_lookup_key") {
		t.Fatalf("expected no table or covering-index scan, got:\n%s", plan)
	}
}

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

func TestMVCCRejectsMultiprocessWAL(t *testing.T) {
	db := openTurso(t, multiprocessWALDSN(filepath.Join(t.TempDir(), "mvcc-multiprocess-wal.db")))
	defer db.Close()

	var mode string
	err := db.QueryRow(`pragma journal_mode='mvcc'`).Scan(&mode)
	if err == nil {
		t.Fatalf("expected MVCC plus multiprocess WAL to be rejected, got journal_mode %q", mode)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "mvcc") {
		t.Fatalf("expected MVCC rejection, got %v", err)
	}
}

func TestMVCCSupportsBeginConcurrent(t *testing.T) {
	ctx := context.Background()
	db := openTurso(t, filepath.Join(t.TempDir(), "mvcc-begin-concurrent.db"))
	defer db.Close()

	var mode string
	if err := db.QueryRowContext(ctx, `pragma journal_mode='mvcc'`).Scan(&mode); err != nil {
		t.Fatalf("failed to enable MVCC: %v", err)
	}
	if mode != "mvcc" {
		t.Fatalf("expected journal mode mvcc, got %q", mode)
	}
	if _, err := db.ExecContext(ctx, `create table writes (id integer primary key, value text not null)`); err != nil {
		t.Fatalf("failed to create writes table: %v", err)
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("failed to reserve connection: %v", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN CONCURRENT"); err != nil {
		t.Fatalf("expected BEGIN CONCURRENT to work in MVCC mode: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `insert into writes (value) values ('committed')`); err != nil {
		t.Fatalf("failed to insert inside BEGIN CONCURRENT: %v", err)
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		t.Fatalf("failed to commit BEGIN CONCURRENT transaction: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, `select count(*) from writes`).Scan(&count); err != nil {
		t.Fatalf("failed to count committed rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one committed row, got %d", count)
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

func TestNativeFTSPlainTursoDBCLI(t *testing.T) {
	tursodb := os.Getenv("TURSO_V072_TURSODB_BIN")
	if tursodb == "" {
		t.Skip("set TURSO_V072_TURSODB_BIN to a Turso 0.7.2 tursodb binary to run the CLI native FTS probe")
	}

	dbPath := filepath.Join(t.TempDir(), "native-fts-cli.db")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	create := exec.CommandContext(ctx, tursodb, "--experimental-index-method", "-q", dbPath, strings.Join([]string{
		"create table docs (id text primary key, body text not null)",
		"create index docs_fts on docs using fts (id, body) with (tokenizer = 'ngram')",
		"insert into docs (id, body) values ('a', 'alpha beta')",
	}, "; "))
	createOut, err := create.CombinedOutput()
	if err != nil {
		t.Fatalf("tursodb should create and write a plain native FTS index: %v\n%s", err, createOut)
	}

	query := exec.CommandContext(ctx, tursodb, "--experimental-index-method", "-q", dbPath, "select id from docs where (id, body) match 'alpha';")
	queryOut, err := query.CombinedOutput()
	if err != nil {
		t.Fatalf("tursodb should query a plain native FTS index: %v\n%s", err, queryOut)
	}
	if !strings.Contains(string(queryOut), "a") {
		t.Fatalf("expected native FTS query to return document a, got:\n%s", queryOut)
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
	if os.Getenv("TURSO_V072_CHILD") == "1" {
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
	if os.Getenv("TURSO_V072_CONCURRENT_CHILD") == "1" {
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

func TestExperimentalMultiProcessWALAllowsConcurrentGoProcesses(t *testing.T) {
	if os.Getenv("TURSO_V072_MULTIPROCESS_WAL_CHILD") == "1" {
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
				"TestExperimentalMultiProcessWALAllowsConcurrentGoProcesses",
				"TURSO_V072_MULTIPROCESS_WAL_CHILD=1",
				"TURSO_V072_DSN="+multiprocessWALDSN(dbPath),
			)
		}()
	}

	var failures []error
	for range processes {
		if err := <-errs; err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		t.Fatalf("expected concurrent Go child writers to succeed with multiprocess WAL, got %d failures; first: %v", len(failures), failures[0])
	}

	verify := openTurso(t, multiprocessWALDSN(dbPath))
	defer verify.Close()

	var count int
	if err := verify.QueryRow(`select count(*) from writes`).Scan(&count); err != nil {
		t.Fatalf("failed to count multiprocess WAL writes: %v", err)
	}
	if want := processes * writesPerProcess; count != want {
		t.Fatalf("expected %d rows after multiprocess WAL writes, got %d", want, count)
	}
}

func TestExperimentalMultiProcessWALAllowsTursoDBCLIPlainProbe(t *testing.T) {
	tursodb := os.Getenv("TURSO_V072_TURSODB_BIN")
	if tursodb == "" {
		t.Skip("set TURSO_V072_TURSODB_BIN to a Turso 0.7.2 tursodb binary to run the CLI multiprocess WAL probe")
	}
	if os.Getenv("TURSO_V072_HOLDER") == "1" {
		runLiveHolder(t)
		return
	}

	dbPath := filepath.Join(t.TempDir(), "live-app.db")
	readyPath := dbPath + ".ready"

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	holder := exec.CommandContext(ctx, os.Args[0], "-test.run=TestExperimentalMultiProcessWALAllowsTursoDBCLIPlainProbe")
	holder.Env = append(os.Environ(),
		"TURSO_V072_HOLDER=1",
		"TURSO_V072_DSN="+multiprocessWALDSN(dbPath),
		"TURSO_V072_READY="+readyPath,
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
