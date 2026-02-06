package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MigrationFunc is a function that performs a migration
type MigrationFunc func(context.Context, *sql.DB) error

// Migration represents a database migration
type Migration struct {
	Up          MigrationFunc
	Down        MigrationFunc
	Version     string
	Description string
}

// MigrationStatus represents the status of a migration
type MigrationStatus struct {
	ExecutedAt  *time.Time
	Version     string
	Description string
	DurationMs  int64
}

// registry stores all registered migrations
var (
	registry   = make(map[string]Migration)
	registryMu sync.RWMutex
)

// Register registers a migration
// This should be called from init() functions in migration files
func Register(m Migration) {
	registryMu.Lock()
	defer registryMu.Unlock()

	if _, exists := registry[m.Version]; exists {
		panic(fmt.Sprintf("migration version %s already registered", m.Version))
	}

	if m.Version == "" {
		panic("migration version cannot be empty")
	}

	if m.Up == nil {
		panic(fmt.Sprintf("migration %s missing Up function", m.Version))
	}

	registry[m.Version] = m
}

// getRegisteredMigrations returns all registered migrations sorted by version
func getRegisteredMigrations() []Migration {
	registryMu.RLock()
	defer registryMu.RUnlock()

	migrations := make([]Migration, 0, len(registry))
	for _, m := range registry {
		migrations = append(migrations, m)
	}

	// Sort by version (timestamp-based versions sort correctly lexicographically)
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations
}

// ensureMigrationsTable creates the _migrations table if it doesn't exist
func ensureMigrationsTable(ctx context.Context, db *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS _migrations (
			version TEXT PRIMARY KEY,
			description TEXT NOT NULL,
			executed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			duration_ms INTEGER NOT NULL
		)
	`

	_, err := db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create _migrations table: %w", err)
	}

	return nil
}

// getExecutedMigrations returns all executed migrations from the database
func getExecutedMigrations(ctx context.Context, db *sql.DB) (map[string]MigrationStatus, error) {
	err := ensureMigrationsTable(ctx, db)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT version, description, executed_at, duration_ms
		FROM _migrations
		ORDER BY version
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	executed := make(map[string]MigrationStatus)
	for rows.Next() {
		var status MigrationStatus
		var executedAt string

		err = rows.Scan(&status.Version, &status.Description, &executedAt, &status.DurationMs)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration row: %w", err)
		}

		// Parse the timestamp
		parsedTime, parseErr := time.Parse("2006-01-02 15:04:05", executedAt)
		if parseErr != nil {
			// Try with timezone
			parsedTime, parseErr = time.Parse(time.RFC3339, executedAt)
			if parseErr != nil {
				return nil, fmt.Errorf("failed to parse executed_at: %w", parseErr)
			}
		}
		status.ExecutedAt = &parsedTime

		executed[status.Version] = status
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("error iterating migration rows: %w", err)
	}

	return executed, nil
}

// recordMigration records a migration execution in the database
func recordMigration(ctx context.Context, tx *sql.Tx, version, description string, durationMs int64) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO _migrations (version, description, duration_ms)
		VALUES (?, ?, ?)
	`, version, description, durationMs)

	if err != nil {
		return fmt.Errorf("failed to record migration: %w", err)
	}

	return nil
}

// removeMigration removes a migration record from the database
func removeMigration(ctx context.Context, tx *sql.Tx, version string) error {
	result, err := tx.ExecContext(ctx, `
		DELETE FROM _migrations WHERE version = ?
	`, version)

	if err != nil {
		return fmt.Errorf("failed to remove migration record: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("migration version %s not found in database", version)
	}

	return nil
}

// Reset clears the migration registry (for testing purposes only)
func Reset() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Migration)
}
