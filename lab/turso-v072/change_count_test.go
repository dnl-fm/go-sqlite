package tursov072

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMVCCSequenceMaintenanceDoesNotLeakChangeCount(t *testing.T) {
	ctx := context.Background()
	db := openTurso(t, filepath.Join(t.TempDir(), "mvcc-sequence-changes.db"))
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("failed to reserve connection: %v", err)
	}
	defer conn.Close()

	var mode string
	if err := conn.QueryRowContext(ctx, `pragma journal_mode='mvcc'`).Scan(&mode); err != nil {
		t.Fatalf("failed to enable MVCC: %v", err)
	}
	if mode != "mvcc" {
		t.Fatalf("expected journal mode mvcc, got %q", mode)
	}
	if _, err := conn.ExecContext(ctx, `create table writes (id integer primary key autoincrement, value text not null)`); err != nil {
		t.Fatalf("failed to create AUTOINCREMENT table: %v", err)
	}

	// Switching an on-disk database into MVCC contributes one connection-local
	// total change before this table exists. Measure insert deltas from that
	// setup baseline; RowsAffected and changes() remain absolute.
	var baselineTotal int64
	if err := conn.QueryRowContext(ctx, `select total_changes()`).Scan(&baselineTotal); err != nil {
		t.Fatalf("failed to read total_changes baseline: %v", err)
	}

	assertInsertCounts := func(statement string, wantChanges, wantTotalDelta int64) {
		t.Helper()

		result, err := conn.ExecContext(ctx, statement)
		if err != nil {
			t.Fatalf("insert failed: %v", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			t.Fatalf("failed to read RowsAffected: %v", err)
		}
		if rowsAffected != wantChanges {
			t.Fatalf("RowsAffected = %d, want %d", rowsAffected, wantChanges)
		}

		var changes, totalChanges int64
		if err := conn.QueryRowContext(ctx, `select changes(), total_changes()`).Scan(&changes, &totalChanges); err != nil {
			t.Fatalf("failed to read change counters: %v", err)
		}
		if changes != wantChanges || totalChanges-baselineTotal != wantTotalDelta {
			t.Fatalf(
				"change counters = (%d, total delta %d), want (%d, total delta %d)",
				changes,
				totalChanges-baselineTotal,
				wantChanges,
				wantTotalDelta,
			)
		}
	}

	assertInsertCounts(`insert into writes (value) values ('a')`, 1, 1)
	assertInsertCounts(`insert into writes (value) values ('b')`, 1, 2)
	assertInsertCounts(`insert into writes (value) values ('c'), ('d')`, 2, 4)
}
