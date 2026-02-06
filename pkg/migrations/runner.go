package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Run executes all pending migrations
// Migrations are run in a transaction and rolled back on error
func Run(ctx context.Context, db *sql.DB) error {
	// Ensure migrations table exists
	err := ensureMigrationsTable(ctx, db)
	if err != nil {
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
		runErr := runMigration(ctx, db, m)
		if runErr != nil {
			return fmt.Errorf("migration %s failed: %w", m.Version, runErr)
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
			tx.Rollback() //nolint:errcheck // best-effort rollback after error
		}
	}()

	durationMs := duration.Milliseconds()
	err = recordMigration(ctx, tx, m.Version, m.Description, durationMs)
	if err != nil {
		return err
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Rollback rolls back migrations to a specific version
// If version is empty, rolls back the last migration
func Rollback(ctx context.Context, db *sql.DB, version string) error {
	// Ensure migrations table exists
	err := ensureMigrationsTable(ctx, db)
	if err != nil {
		return err
	}

	// Get executed migrations
	executed, err := getExecutedMigrations(ctx, db)
	if err != nil {
		return err
	}

	if len(executed) == 0 {
		return errors.New("no migrations to rollback")
	}

	// Determine which versions to rollback
	toRollback, err := resolveRollbackTargets(executed, version)
	if err != nil {
		return err
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

		rbErr := rollbackMigration(ctx, db, m)
		if rbErr != nil {
			return fmt.Errorf("rollback of %s failed: %w", v, rbErr)
		}
	}

	return nil
}

// resolveRollbackTargets determines which versions need to be rolled back.
func resolveRollbackTargets(executed map[string]MigrationStatus, version string) ([]string, error) {
	// Build sorted list of executed versions
	versions := make([]string, 0, len(executed))
	for v := range executed {
		versions = append(versions, v)
	}
	sort.Strings(versions)

	// Determine target version
	var targetVersion string
	if version == "" {
		targetVersion = versions[len(versions)-1]
	} else {
		if _, exists := executed[version]; !exists {
			return nil, fmt.Errorf("migration version %s not found in executed migrations", version)
		}
		targetVersion = version
	}

	// Collect versions >= target in reverse order
	var toRollback []string
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		toRollback = append(toRollback, v)
		if v == targetVersion {
			break
		}
	}

	return toRollback, nil
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
			tx.Rollback() //nolint:errcheck // best-effort rollback after error
		}
	}()

	err = removeMigration(ctx, tx, m.Version)
	if err != nil {
		return err
	}

	// Commit transaction
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Status returns the status of all migrations
func Status(ctx context.Context, db *sql.DB) ([]MigrationStatus, error) {
	// Ensure migrations table exists
	err := ensureMigrationsTable(ctx, db)
	if err != nil {
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
	statuses := make([]MigrationStatus, 0, len(registered))
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
	err := ensureMigrationsTable(ctx, db)
	if err != nil {
		return "", err
	}

	var version string
	err = db.QueryRowContext(ctx, `
		SELECT version FROM _migrations
		ORDER BY version DESC
		LIMIT 1
	`).Scan(&version)

	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // No migrations executed yet
	}

	if err != nil {
		return "", fmt.Errorf("failed to get latest migration: %w", err)
	}

	return version, nil
}
