package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/dnl-fm/go-sqlite/pkg/driver/modernc"

	"github.com/dnl-fm/go-sqlite/pkg/migrations"
)

func main() {
	fmt.Println("=== Migrations Example ===")
	fmt.Println()

	ctx := context.Background()

	// Open database connection
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	fmt.Println("✓ Database connection established")
	fmt.Println()

	// Register example migrations
	registerMigrations()

	// Demonstrate migration operations
	fmt.Println("--- Initial Migration Status ---")
	if err := showStatus(ctx, db); err != nil {
		log.Fatalf("Failed to show status: %v", err)
	}

	// Run all migrations
	fmt.Println()
	fmt.Println("--- Running Migrations ---")
	if err := runMigrations(ctx, db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Show status after running
	fmt.Println()
	fmt.Println("--- Status After Running ---")
	if err := showStatus(ctx, db); err != nil {
		log.Fatalf("Failed to show status: %v", err)
	}

	// Show latest version
	fmt.Println()
	fmt.Println("--- Latest Migration Version ---")
	if err := showLatest(ctx, db); err != nil {
		log.Fatalf("Failed to show latest: %v", err)
	}

	// Verify tables created
	fmt.Println()
	fmt.Println("--- Verifying Tables ---")
	if err := verifyTables(ctx, db); err != nil {
		log.Fatalf("Failed to verify tables: %v", err)
	}

	// Rollback demonstration
	fmt.Println()
	fmt.Println("--- Rolling Back Last Migration ---")
	if err := rollbackMigration(ctx, db); err != nil {
		log.Fatalf("Failed to rollback: %v", err)
	}

	// Show status after rollback
	fmt.Println()
	fmt.Println("--- Status After Rollback ---")
	if err := showStatus(ctx, db); err != nil {
		log.Fatalf("Failed to show status: %v", err)
	}

	// Run migrations again
	fmt.Println()
	fmt.Println("--- Running Migrations Again (Idempotent) ---")
	if err := runMigrations(ctx, db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	fmt.Println()
	fmt.Println("✅ All migration operations completed successfully!")
}

func registerMigrations() {
	fmt.Println("Registering migrations...")

	// Migration 1: Create users table
	migrations.Register(migrations.Migration{
		Version:     "20251107000001",
		Description: "create_users_table",
		Up:          up20251107000001,
		Down:        down20251107000001,
	})

	// Migration 2: Create posts table
	migrations.Register(migrations.Migration{
		Version:     "20251107000002",
		Description: "create_posts_table",
		Up:          up20251107000002,
		Down:        down20251107000002,
	})

	// Migration 3: Add indexes
	migrations.Register(migrations.Migration{
		Version:     "20251107000003",
		Description: "add_indexes",
		Up:          up20251107000003,
		Down:        down20251107000003,
	})

	fmt.Println("✓ Registered 3 migrations")
	fmt.Println()
}

// Migration 1: Create users table
func up20251107000001(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			username TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create users table: %w", err)
	}
	fmt.Println("   ✓ Created users table")
	return nil
}

func down20251107000001(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS users`)
	if err != nil {
		return fmt.Errorf("failed to drop users table: %w", err)
	}
	fmt.Println("   ✓ Dropped users table")
	return nil
}

// Migration 2: Create posts table
func up20251107000002(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			title TEXT NOT NULL,
			content TEXT,
			views INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create posts table: %w", err)
	}
	fmt.Println("   ✓ Created posts table")
	return nil
}

func down20251107000002(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS posts`)
	if err != nil {
		return fmt.Errorf("failed to drop posts table: %w", err)
	}
	fmt.Println("   ✓ Dropped posts table")
	return nil
}

// Migration 3: Add indexes
func up20251107000003(ctx context.Context, db *sql.DB) error {
	// Create index on users.email
	_, err := db.ExecContext(ctx, `
		CREATE INDEX idx_users_email ON users(email)
	`)
	if err != nil {
		return fmt.Errorf("failed to create users email index: %w", err)
	}

	// Create index on posts.user_id
	_, err = db.ExecContext(ctx, `
		CREATE INDEX idx_posts_user_id ON posts(user_id)
	`)
	if err != nil {
		return fmt.Errorf("failed to create posts user_id index: %w", err)
	}

	fmt.Println("   ✓ Created indexes on users.email and posts.user_id")
	return nil
}

func down20251107000003(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_users_email`)
	if err != nil {
		return fmt.Errorf("failed to drop users email index: %w", err)
	}

	_, err = db.ExecContext(ctx, `DROP INDEX IF EXISTS idx_posts_user_id`)
	if err != nil {
		return fmt.Errorf("failed to drop posts user_id index: %w", err)
	}

	fmt.Println("   ✓ Dropped indexes")
	return nil
}

func showStatus(ctx context.Context, db *sql.DB) error {
	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		return err
	}

	if len(statuses) == 0 {
		fmt.Println("No migrations found")
		return nil
	}

	fmt.Printf("%-20s %-30s %-15s %s\n", "Version", "Description", "Status", "Executed At")
	fmt.Println("--------------------------------------------------------------------------------")

	for _, status := range statuses {
		executedAt := "pending"
		if status.ExecutedAt != nil {
			executedAt = status.ExecutedAt.Format("2006-01-02 15:04:05")
		}

		statusStr := "pending"
		if status.ExecutedAt != nil {
			statusStr = "executed"
		}

		fmt.Printf("%-20s %-30s %-15s %s\n",
			status.Version,
			status.Description,
			statusStr,
			executedAt,
		)
	}

	return nil
}

func runMigrations(ctx context.Context, db *sql.DB) error {
	err := migrations.Run(ctx, db)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	fmt.Println("✓ All pending migrations executed successfully")
	return nil
}

func showLatest(ctx context.Context, db *sql.DB) error {
	version, err := migrations.Latest(ctx, db)
	if err != nil {
		return err
	}

	if version == "" {
		fmt.Println("No migrations executed yet")
	} else {
		fmt.Printf("Latest migration version: %s\n", version)
	}

	return nil
}

func verifyTables(ctx context.Context, db *sql.DB) error {
	// Query to get all tables
	rows, err := db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='table' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("Tables in database:")
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return err
		}
		fmt.Printf("   - %s\n", tableName)
	}

	// Query to get indexes
	rows, err = db.QueryContext(ctx, `
		SELECT name FROM sqlite_master
		WHERE type='index' AND name NOT LIKE 'sqlite_%'
		ORDER BY name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Println("Indexes in database:")
	for rows.Next() {
		var indexName string
		if err := rows.Scan(&indexName); err != nil {
			return err
		}
		fmt.Printf("   - %s\n", indexName)
	}

	return nil
}

func rollbackMigration(ctx context.Context, db *sql.DB) error {
	// Get current version
	version, err := migrations.Latest(ctx, db)
	if err != nil {
		return err
	}

	if version == "" {
		fmt.Println("No migrations to rollback")
		return nil
	}

	fmt.Printf("Rolling back migration: %s\n", version)

	// Rollback to previous version (empty string rolls back last)
	err = migrations.Rollback(ctx, db, "")
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Println("✓ Rollback successful")
	return nil
}
