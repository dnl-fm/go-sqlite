package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func TestRollback(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Register migrations
	Register(Migration{
		Version:     "20251107000001",
		Description: "create_users",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL)
			`)
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
				CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL)
			`)
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

	// Rollback last migration
	err = Rollback(ctx, db, "20251107000002")
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify posts table was dropped
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='posts'`).Scan(&count)
	if err != nil || count != 0 {
		t.Errorf("posts table should be dropped")
	}

	// Verify users table still exists
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'`).Scan(&count)
	if err != nil || count != 1 {
		t.Errorf("users table should still exist")
	}

	// Verify migration record was removed
	var migrationCount int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM _migrations`).Scan(&migrationCount)
	if err != nil {
		t.Fatalf("failed to count migrations: %v", err)
	}
	if migrationCount != 1 {
		t.Errorf("expected 1 migration remaining, got %d", migrationCount)
	}
}

// TestStatus tests getting migration status

func TestRollbackWithoutDownFunction(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Register migration without Down function
	Register(Migration{
		Version:     "20251107000001",
		Description: "no_down",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `
				CREATE TABLE test_table (id TEXT PRIMARY KEY)
			`)
			return err
		},
		Down: nil, // No down function
	})

	// Run migration
	err := Run(ctx, db)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Attempt rollback should fail
	err = Rollback(ctx, db, "20251107000001")
	if err == nil {
		t.Fatal("expected Rollback to fail when Down function is missing")
	}
}
