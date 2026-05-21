package migrations

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Register test migrations
	executed := []string{}

	Register(Migration{
		Version:     "20251107000001",
		Description: "create_users",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE users (
					id TEXT PRIMARY KEY,
					name TEXT NOT NULL
				)
			`)
			executed = append(executed, "20251107000001_up")
			return err
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `DROP TABLE users`)
			return err
		},
	})

	Register(Migration{
		Version:     "20251107000002",
		Description: "create_posts",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE posts (
					id TEXT PRIMARY KEY,
					user_id TEXT NOT NULL,
					title TEXT NOT NULL
				)
			`)
			executed = append(executed, "20251107000002_up")
			return err
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `DROP TABLE posts`)
			return err
		},
	})

	// Run migrations
	err := Run(ctx, db)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify migrations were executed in order
	if len(executed) != 2 {
		t.Errorf("expected 2 migrations executed, got %d", len(executed))
	}
	if executed[0] != "20251107000001_up" {
		t.Errorf("first migration should be 20251107000001_up, got %s", executed[0])
	}
	if executed[1] != "20251107000002_up" {
		t.Errorf("second migration should be 20251107000002_up, got %s", executed[1])
	}

	// Verify tables were created
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("users table not created")
	}

	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("posts table not created")
	}

	// Verify migrations were recorded
	var migrationCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _migrations`).Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 2 {
		t.Errorf("expected 2 migrations recorded, got %d", migrationCount)
	}

	// Running again should be idempotent (no error, no duplicate execution)
	executed = []string{}
	err = Run(ctx, db)
	if err != nil {
		t.Errorf("second Run should succeed: %v", err)
	}
	if len(executed) != 0 {
		t.Errorf("second Run should not execute any migrations, executed %d", len(executed))
	}
}

func TestRunRejectsWithoutRowIDMigration(t *testing.T) {
	Reset()
	defer Reset()

	ctx := context.Background()
	db := setupTestDB(t)
	defer db.Close()

	Register(Migration{
		Version:     "20251107000003",
		Description: "without_rowid",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE lookup (
					id TEXT PRIMARY KEY
				) WITHOUT ROWID
			`)
			return err
		},
	})

	err := Run(ctx, db)
	if err == nil {
		t.Fatal("expected WITHOUT ROWID migration to fail")
	}
	if !strings.Contains(err.Error(), "WITHOUT ROWID") && !strings.Contains(err.Error(), "requires rowid tables") {
		t.Fatalf("expected rowid requirement error, got %v", err)
	}
}

// TestRollback tests rolling back migrations

func TestMigrationError(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Register migration that will fail
	Register(Migration{
		Version:     "20251107000001",
		Description: "failing_migration",
		Up: func(ctx context.Context, db *sql.DB) error {
			// Cause an error (invalid SQL)
			_, err := db.ExecContext(ctx, `INVALID SQL`)
			return err
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			return nil
		},
	})

	// Run should fail
	err := Run(ctx, db)
	if err == nil {
		t.Fatal("expected Run to fail")
	}

	// Verify migration was NOT recorded
	var migrationCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _migrations`).Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 0 {
		t.Errorf("expected 0 migrations recorded, got %d", migrationCount)
	}
}

// TestRollbackWithoutDownFunction tests error handling when Down is missing
