package migrations

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// setupTestDB creates an in-memory database for testing
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	return db
}

// TestRegister tests migration registration
func TestRegister(t *testing.T) {
	// Reset registry before test
	Reset()
	defer Reset()

	tests := []struct {
		migration   Migration
		name        string
		panicMsg    string
		expectPanic bool
	}{
		{
			name: "valid migration",
			migration: Migration{
				Version:     "20251107000001",
				Description: "test_migration",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
				Down: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: false,
		},
		{
			name: "duplicate version",
			migration: Migration{
				Version:     "20251107000001",
				Description: "duplicate",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: true,
			panicMsg:    "already registered",
		},
		{
			name: "empty version",
			migration: Migration{
				Version:     "",
				Description: "empty_version",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: true,
			panicMsg:    "cannot be empty",
		},
		{
			name: "missing up function",
			migration: Migration{
				Version:     "20251107000002",
				Description: "no_up",
				Up:          nil,
			},
			expectPanic: true,
			panicMsg:    "missing Up function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic but didn't get one")
					}
				}()
			}

			Register(tt.migration)

			if !tt.expectPanic {
				// Verify migration was registered
				registryMu.RLock()
				_, exists := registry[tt.migration.Version]
				registryMu.RUnlock()

				if !exists {
					t.Errorf("migration not found in registry")
				}
			}
		})
	}
}

// TestRun tests running pending migrations
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

// TestRollback tests rolling back migrations
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
