package tenants

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Scenarios 06-09: migrations, backup, crash recovery, ATTACH limit.

// schemaV2 is two statements so the test can prove a step is atomic: on the
// poisoned tenant the ALTER succeeds, the CREATE fails, and both roll back.
const schemaV2 = `ALTER TABLE events ADD COLUMN note TEXT NOT NULL DEFAULT '';
CREATE TABLE tags (event_id INTEGER NOT NULL REFERENCES events(id), tag TEXT NOT NULL)`

func TestScenario06_Migrations(t *testing.T) {
	const tenants, poisoned = 20, "t0007"
	root := labRoot(t)
	v1 := newManager(t, root, 0, schemaV1)
	ids := tenantIDs(tenants)
	mustCreate(t, v1, ids...)
	for _, id := range ids {
		if err := insert(bg, v1, id, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	// Someone hot-fixed this tenant by hand. Its v2 step will collide.
	err := v1.With(bg, poisoned, func(tn *Tenant) error {
		return tn.Write(bg, func(tx *sql.Tx) error {
			_, err := tx.Exec(`CREATE TABLE tags (oops TEXT)`)
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	// Ship v2.
	v2 := newManager(t, root, 0, schemaV1, schemaV2)
	results, err := v2.MigrateAll(bg, 8)
	if err != nil || len(results) != tenants {
		t.Fatalf("MigrateAll = %d results, %v", len(results), err)
	}
	for _, r := range results {
		var migErr *MigrationError
		switch {
		case r.ID != poisoned && r.Err != nil:
			t.Errorf("%s: %v", r.ID, r.Err)
		case r.ID == poisoned && !errors.As(r.Err, &migErr):
			t.Errorf("poisoned tenant: %v, want *MigrationError", r.Err)
		case r.ID == poisoned && (migErr.Tenant != poisoned || migErr.Version != 1):
			t.Errorf("poisoned tenant: %+v, want tenant %s stuck at version 1", migErr, poisoned)
		}
	}

	// 19 tenants serve at v2, using the new schema.
	for _, id := range ids {
		if id == poisoned {
			continue
		}
		if v := mustQueryInt(t, v2, id, "PRAGMA user_version"); v != 2 {
			t.Errorf("%s at version %d, want 2", id, v)
		}
		err := v2.With(bg, id, func(tn *Tenant) error {
			return tn.Write(bg, func(tx *sql.Tx) error {
				_, err := tx.Exec(`INSERT INTO tags (event_id, tag) SELECT id, note || 'v2' FROM events`)
				return err
			})
		})
		if err != nil {
			t.Errorf("%s does not serve v2: %v", id, err)
		}
	}

	// The poisoned tenant is not served, by With or by a second MigrateAll.
	var migErr *MigrationError
	if err := insert(bg, v2, poisoned, 0, 1); !errors.As(err, &migErr) {
		t.Fatalf("With(poisoned) = %v, want *MigrationError", err)
	}
	if s := v2.Stats(); s.Open != tenants-1 {
		t.Errorf("open handles = %d, want %d: the failed tenant must not stay open", s.Open, tenants-1)
	}
	results, _ = v2.MigrateAll(bg, 8)
	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
		}
	}
	if failed != 1 {
		t.Errorf("second MigrateAll: %d failures, want 1", failed)
	}

	// It is untouched at v1: version 1, no half-applied ALTER, data intact.
	old := newManager(t, labRootCopy(t, root, poisoned), 0, schemaV1)
	if v := mustQueryInt(t, old, poisoned, "PRAGMA user_version"); v != 1 {
		t.Errorf("poisoned tenant at version %d, want 1", v)
	}
	if n := mustQueryInt(t, old, poisoned, `SELECT count(*) FROM pragma_table_info('events') WHERE name = 'note'`); n != 0 {
		t.Error("poisoned tenant kept the ALTER from the failed step")
	}
	if n := mustQueryInt(t, old, poisoned, countSQL); n != 1 {
		t.Errorf("poisoned tenant has %d rows, want 1", n)
	}
	integrityCheck(t, old, poisoned)

	// Repair the tenant and it migrates on its next open. No restart needed.
	if err := old.With(bg, poisoned, func(tn *Tenant) error {
		return tn.Write(bg, func(tx *sql.Tx) error {
			_, err := tx.Exec(`DROP TABLE tags`)
			return err
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(old.root, poisoned+".db"), filepath.Join(root, poisoned+".db")); err != nil {
		t.Fatal(err)
	}
	if v := mustQueryInt(t, v2, poisoned, "PRAGMA user_version"); v != 2 {
		t.Errorf("repaired tenant at version %d, want 2", v)
	}
}

// labRootCopy copies one closed tenant database into a new root, so a test
// can open it with a different migration list.
func labRootCopy(t *testing.T, root, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, id+".db"))
	if err != nil {
		t.Fatal(err)
	}
	dst := labRoot(t)
	if err := os.WriteFile(filepath.Join(dst, id+".db"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestScenario06_MigrateOnOpen(t *testing.T) {
	root := labRoot(t)
	v1 := newManager(t, root, 0, schemaV1)
	mustCreate(t, v1, "acme")
	if err := insert(bg, v1, "acme", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	// No MigrateAll: the first With after the deploy migrates the tenant.
	v2 := newManager(t, root, 0, schemaV1, schemaV2)
	err := v2.With(bg, "acme", func(tn *Tenant) error {
		return tn.Write(bg, func(tx *sql.Tx) error {
			_, err := tx.Exec(`INSERT INTO tags (event_id, tag) SELECT id, 'first use' FROM events`)
			return err
		})
	})
	if err != nil {
		t.Fatalf("first use after deploy: %v", err)
	}
	if v := mustQueryInt(t, v2, "acme", "PRAGMA user_version"); v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
	// New tenants are born at the current version.
	mustCreate(t, v2, "newco")
	if v := mustQueryInt(t, v2, "newco", "PRAGMA user_version"); v != 2 {
		t.Fatalf("new tenant version = %d, want 2", v)
	}
	if err := v2.Close(); err != nil {
		t.Fatal(err)
	}

	// Rolling back the binary: a tenant from the future is refused, not served.
	var migErr *MigrationError
	if err := insert(bg, newManager(t, root, 0, schemaV1), "acme", 0, 1); !errors.As(err, &migErr) || migErr.Version != 2 {
		t.Fatalf("old binary on new tenant = %v, want *MigrationError at version 2", err)
	}
}

func TestScenario07_BackupUnderWrite(t *testing.T) {
	const writers, rowsPerTx, backups = 4, 3, 3
	root, backupRoot := labRoot(t), labRoot(t)
	m := newManager(t, root, 0, schemaV1)
	mustCreate(t, m, "acme")

	// Every transaction appends rowsPerTx rows numbered count+1... so any
	// snapshot taken at a commit boundary has count == max(seq) and
	// count % rowsPerTx == 0. A torn copy breaks one of them.
	writeTx := func(w int) error {
		return m.With(bg, "acme", func(tn *Tenant) error {
			return tn.Write(bg, func(tx *sql.Tx) error {
				for range rowsPerTx {
					if _, err := tx.Exec(`INSERT INTO events (writer, seq) SELECT ?, count(*) + 1 FROM events`, w); err != nil {
						return err
					}
				}
				return nil
			})
		})
	}
	for range 500 { // give the backup something to copy
		if err := writeTx(0); err != nil {
			t.Fatal(err)
		}
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					if err := writeTx(w); err != nil {
						t.Errorf("writer %d: %v", w, err)
						return
					}
				}
			}
		})
	}

	type snapshot struct {
		id            string
		before, after int
	}
	var snapshots []snapshot
	for i := range backups {
		time.Sleep(50 * time.Millisecond)
		s := snapshot{id: fmt.Sprintf("acme-%d", i), before: mustQueryInt(t, m, "acme", countSQL)}
		start := time.Now()
		if err := m.Backup(bg, "acme", filepath.Join(backupRoot, s.id+".db")); err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		record(t, "07-backup", fmt.Sprintf("backup %d under %d writers", i, writers), ms(time.Since(start)), "ms")
		s.after = mustQueryInt(t, m, "acme", countSQL)
		snapshots = append(snapshots, s)
	}
	close(stop)
	wg.Wait()

	if err := m.Backup(bg, "acme", filepath.Join(backupRoot, "acme-0.db")); !errors.Is(err, os.ErrExist) {
		t.Fatalf("Backup over an existing file = %v, want ErrExist", err)
	}
	if err := m.Backup(bg, "ghost", filepath.Join(backupRoot, "ghost.db")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Backup of unknown tenant = %v, want ErrNotFound", err)
	}

	// Restore is "put the file in a root": open the copies as tenants.
	restored := newManager(t, backupRoot, 0, schemaV1)
	for _, s := range snapshots {
		integrityCheck(t, restored, s.id)
		count := mustQueryInt(t, restored, s.id, countSQL)
		maxSeq := mustQueryInt(t, restored, s.id, `SELECT max(seq) FROM events`)
		record(t, "07-backup", s.id+" rows (live before / copy / live after)", fmt.Sprintf("%d / %d / %d", s.before, count, s.after), "rows")
		if count != maxSeq || count%rowsPerTx != 0 {
			t.Errorf("%s is not a commit-boundary snapshot: count %d, max(seq) %d", s.id, count, maxSeq)
		}
		if count < s.before || count > s.after {
			t.Errorf("%s has %d rows, outside the live range [%d, %d]", s.id, count, s.before, s.after)
		}
	}
	integrityCheck(t, m, "acme")
}

const (
	crashBatchSize  = 10
	crashKillAfter  = 300
	crashTenantName = "victim"
)

func TestScenario08_Crash(t *testing.T) {
	if os.Getenv(childEnv) == "crash" {
		crashChild(t, os.Getenv(childRootEnv))
		return
	}
	root := labRoot(t)
	setup := newManager(t, root, 0, schemaV1)
	mustCreate(t, setup, crashTenantName)
	if err := setup.Close(); err != nil {
		t.Fatal(err)
	}

	cmd := childCommand("TestScenario08_Crash", "crash", root)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// The child prints a line after each commit. Kill it mid-stream, then
	// keep reading: every line it managed to write is a commit it was told about.
	acked := 0
	lines := bufio.NewScanner(stdout)
	for lines.Scan() {
		if !strings.HasPrefix(lines.Text(), "committed ") {
			continue
		}
		if acked++; acked == crashKillAfter {
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := cmd.Wait(); err == nil || acked < crashKillAfter {
		t.Fatalf("child was not killed mid-stream: err %v, %d commits acknowledged", err, acked)
	}
	// No clean close happened: the WAL is still there, and recovery has to read it.
	wal, err := os.Stat(filepath.Join(root, crashTenantName+".db-wal"))
	if err != nil || wal.Size() == 0 {
		t.Fatalf("expected a non-empty WAL after SIGKILL: %v", err)
	}

	m := newManager(t, root, 0, schemaV1)
	integrityCheck(t, m, crashTenantName)
	batches := mustQueryInt(t, m, crashTenantName, `SELECT count(DISTINCT writer) FROM events`)
	rows := mustQueryInt(t, m, crashTenantName, countSQL)
	torn := mustQueryInt(t, m, crashTenantName, fmt.Sprintf(
		`SELECT count(*) FROM (SELECT writer FROM events GROUP BY writer HAVING count(*) != %d)`, crashBatchSize))
	lastBatch := mustQueryInt(t, m, crashTenantName, `SELECT max(writer) FROM events`)
	record(t, "08-crash", "commits acknowledged before SIGKILL", acked, "tx")
	record(t, "08-crash", "transactions present after reopen", batches, "tx")
	record(t, "08-crash", "WAL size at reopen", wal.Size(), "bytes")
	if torn != 0 || rows != batches*crashBatchSize {
		t.Errorf("%d torn transactions, %d rows in %d batches", torn, rows, batches)
	}
	if lastBatch != batches-1 {
		t.Errorf("batches are not contiguous: %d batches, last is %d", batches, lastBatch)
	}
	// Every acknowledged commit survived; at most one commit was in flight unacknowledged.
	if batches < acked || batches > acked+1 {
		t.Errorf("%d transactions on disk, %d acknowledged", batches, acked)
	}
	// And the tenant is writable again.
	if err := insert(bg, m, crashTenantName, batches, 0); err != nil {
		t.Fatalf("write after recovery: %v", err)
	}
}

// crashChild writes batches of rows, one transaction each, until it is killed.
func crashChild(t *testing.T, root string) {
	m, err := New(Config{Root: root, Migrations: []string{schemaV1}})
	if err != nil {
		t.Fatal(err)
	}
	for batch := 0; ; batch++ {
		err := m.With(bg, crashTenantName, func(tn *Tenant) error {
			return tn.Write(bg, func(tx *sql.Tx) error {
				for seq := range crashBatchSize {
					if _, err := tx.Exec(insertSQL, batch, seq); err != nil {
						return err
					}
				}
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		fmt.Printf("committed %d\n", batch)
	}
}

func TestScenario09_AttachLimit(t *testing.T) {
	const tenants = 12
	root := labRoot(t)
	m := newManager(t, root, 0, schemaV1)
	ids := tenantIDs(tenants)
	mustCreate(t, m, ids...)
	for i, id := range ids {
		if err := insert(bg, m, id, i, 0); err != nil {
			t.Fatal(err)
		}
	}

	// The tempting way to query across tenants: one connection, ATTACH them all.
	db, err := sql.Open("sqlite", filepath.Join(root, ids[0]+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	attached := 0
	var attachErr error
	for i, id := range ids[1:] {
		if _, attachErr = db.Exec(fmt.Sprintf(`ATTACH DATABASE ? AS t%d`, i), filepath.Join(root, id+".db")); attachErr != nil {
			break
		}
		attached++
	}
	record(t, "09-attach", "databases attached before the limit", attached, "databases")
	record(t, "09-attach", "error on the next ATTACH", fmt.Sprint(attachErr), "")
	if attached != 10 || attachErr == nil || !strings.Contains(attachErr.Error(), "too many attached databases") {
		t.Fatalf("attached %d, then %v; want 10, then 'too many attached databases'", attached, attachErr)
	}

	// The way that scales: fan out, one tenant at a time, merge in Go.
	var rows atomic.Int64
	results, err := m.ForEach(bg, 4, func(tn *Tenant) error {
		return tn.Read(bg, func(tx *sql.Tx) error {
			var n int64
			err := tx.QueryRow(countSQL).Scan(&n)
			rows.Add(n)
			return err
		})
	})
	if err != nil || len(results) != tenants {
		t.Fatalf("ForEach = %d results, %v", len(results), err)
	}
	for _, r := range results {
		if r.Err != nil {
			t.Errorf("%s: %v", r.ID, r.Err)
		}
	}
	if rows.Load() != tenants {
		t.Fatalf("ForEach saw %d rows, want %d", rows.Load(), tenants)
	}
}
