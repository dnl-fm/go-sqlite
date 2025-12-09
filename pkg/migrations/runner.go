package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Run executes all pending migrations
// Migrations are run in a transaction and rolled back on error
func Run(ctx context.Context, db *sql.DB) error {
	// Ensure migrations table exists
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}

	// Get executed migrations
	executed, err := getExecutedMigrations(ctx, db)
	if err != nil {
		return err
	}

	// Get registered migrations
	registered := getRegisteredMigrations()

	// Find pending migrations
	var pending []Migration
	for _, m := range registered {
		if _, done := executed[m.Version]; !done {
			pending = append(pending, m)
		}
	}

	if len(pending) == 0 {
		return nil // No pending migrations
	}

	// Execute pending migrations
	for _, m := range pending {
		if err := runMigration(ctx, db, m); err != nil {
			return fmt.Errorf("migration %s failed: %w", m.Version, err)
		}
	}

	return nil
}

// runMigration executes a single migration
// Note: Each migration Up function handles its own transactions if needed
func runMigration(ctx context.Context, db *sql.DB, m Migration) error {
	// Execute migration
	start := time.Now()
	err := m.Up(ctx, db)
	if err != nil {
		return fmt.Errorf("up migration failed: %w", err)
	}
	duration := time.Since(start)

	// Record migration in a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	durationMs := duration.Milliseconds()
	if err = recordMigration(ctx, tx, m.Version, m.Description, durationMs); err != nil {
		return err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Rollback rolls back migrations to a specific version
// If version is empty, rolls back the last migration
func Rollback(ctx context.Context, db *sql.DB, version string) error {
	// Ensure migrations table exists
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return err
	}

	// Get executed migrations
	executed, err := getExecutedMigrations(ctx, db)
	if err != nil {
		return err
	}

	if len(executed) == 0 {
		return fmt.Errorf("no migrations to rollback")
	}

	// Build sorted list of executed versions
	versions := make([]string, 0, len(executed))
	for v := range executed {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	// Determine target version
	var targetVersion string
	if version == "" {
		// Rollback last migration
		if len(versions) == 0 {
			return fmt.Errorf("no migrations to rollback")
		}
		targetVersion = versions[len(versions)-1]
	} else {
		// Validate target version exists
		if _, exists := executed[version]; !exists {
			return fmt.Errorf("migration version %s not found in executed migrations", version)
		}
		targetVersion = version
	}

	// Find migrations to rollback (all versions >= target, in reverse order)
	var toRollback []string
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		toRollback = append(toRollback, v)
		if v == targetVersion {
			break
		}
	}

	// Get registered migrations for Down functions
	registryMu.RLock()
	defer registryMu.RUnlock()

	// Execute rollbacks
	for _, v := range toRollback {
		m, exists := registry[v]
		if !exists {
			return fmt.Errorf("migration %s not found in registry", v)
		}

		if m.Down == nil {
			return fmt.Errorf("migration %s has no Down function", v)
		}

		if err := rollbackMigration(ctx, db, m); err != nil {
			return fmt.Errorf("rollback of %s failed: %w", v, err)
		}
	}

	return nil
}

// rollbackMigration rolls back a single migration
// Note: Each migration Down function handles its own transactions if needed
func rollbackMigration(ctx context.Context, db *sql.DB, m Migration) error {
	// Execute down migration
	err := m.Down(ctx, db)
	if err != nil {
		return fmt.Errorf("down migration failed: %w", err)
	}

	// Remove migration record in a transaction
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			tx.Rollback()
		}
	}()

	if err = removeMigration(ctx, tx, m.Version); err != nil {
		return err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Status returns the status of all migrations
func Status(ctx context.Context, db *sql.DB) ([]MigrationStatus, error) {
	// Ensure migrations table exists
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return nil, err
	}

	// Get executed migrations
	executed, err := getExecutedMigrations(ctx, db)
	if err != nil {
		return nil, err
	}

	// Get registered migrations
	registered := getRegisteredMigrations()

	// Build status list
	var statuses []MigrationStatus
	for _, m := range registered {
		status := MigrationStatus{
			Version:     m.Version,
			Description: m.Description,
		}

		if execStatus, ok := executed[m.Version]; ok {
			status.ExecutedAt = execStatus.ExecutedAt
			status.DurationMs = execStatus.DurationMs
		}

		statuses = append(statuses, status)
	}

	return statuses, nil
}

// Latest returns the latest executed migration version
func Latest(ctx context.Context, db *sql.DB) (string, error) {
	// Ensure migrations table exists
	if err := ensureMigrationsTable(ctx, db); err != nil {
		return "", err
	}

	var version string
	err := db.QueryRowContext(ctx, `
		SELECT version FROM _migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&version)

	if err == sql.ErrNoRows {
		return "", nil // No migrations executed yet
	}

	if err != nil {
		return "", fmt.Errorf("failed to get latest migration: %w", err)
	}

	return version, nil
}
