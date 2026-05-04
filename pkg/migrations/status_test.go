package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestStatus(t *testing.T) {
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
				CREATE TABLE users (id TEXT PRIMARY KEY)
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
				CREATE TABLE posts (id TEXT PRIMARY KEY)
			`)
			return err
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			_, err := db.ExecContext(ctx, `DROP TABLE posts`)
			return err
		},
	})

	// Get status before running migrations
	statuses, err := Status(ctx, db)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if len(statuses) != 2 {
		t.Fatalf("expected 2 statuses, got %d", len(statuses))
	}

	// Both should be pending
	for _, s := range statuses {
		if s.ExecutedAt != nil {
			t.Errorf("migration %s should be pending", s.Version)
		}
	}

	// Run first migration only
	err = runMigration(ctx, db, registry["20251107000001"])
	if err != nil {
		t.Fatalf("runMigration failed: %v", err)
	}

	// Get status after running one migration
	statuses, err = Status(ctx, db)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	// First should be executed, second pending
	if statuses[0].ExecutedAt == nil {
		t.Errorf("first migration should be executed")
	}
	if statuses[1].ExecutedAt != nil {
		t.Errorf("second migration should be pending")
	}

	// Verify executed time is recent
	if time.Since(*statuses[0].ExecutedAt) > 5*time.Second {
		t.Errorf("executed_at time seems incorrect: %v", statuses[0].ExecutedAt)
	}
}

// TestLatest tests getting the latest migration version

func TestLatest(t *testing.T) {
	Reset()
	defer Reset()

	db := setupTestDB(t)
	defer db.Close()

	ctx := context.Background()

	// Initially should return empty string
	version, err := Latest(ctx, db)
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}
	if version != "" {
		t.Errorf("expected empty version, got %s", version)
	}

	// Register and run migrations
	Register(Migration{
		Version:     "20251107000001",
		Description: "create_users",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, execErr := db.ExecContext(ctx, `
				CREATE TABLE users (id TEXT PRIMARY KEY)
			`)
			return execErr
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			return nil
		},
	})

	Register(Migration{
		Version:     "20251107000002",
		Description: "create_posts",
		Up: func(ctx context.Context, db *sql.DB) error {
			_, execErr := db.ExecContext(ctx, `
				CREATE TABLE posts (id TEXT PRIMARY KEY)
			`)
			return execErr
		},
		Down: func(ctx context.Context, db *sql.DB) error {
			return nil
		},
	})

	err = Run(ctx, db)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Should return latest version
	version, err = Latest(ctx, db)
	if err != nil {
		t.Fatalf("Latest failed: %v", err)
	}
	if version != "20251107000002" {
		t.Errorf("expected version 20251107000002, got %s", version)
	}
}

// TestMigrationError tests error handling in migrations
