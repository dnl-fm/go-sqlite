package migrations

import (
	"context"
	"database/sql"

	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

func init() {
	migrations.Register(migrations.Migration{
		Version:     "20251107005645",
		Description: "example_users_table",
		Up:          up20251107005645,
		Down:        down20251107005645,
	})
}

// up20251107005645 runs the up migration
func up20251107005645(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	return err
}

// down20251107005645 rolls back the migration
func down20251107005645(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `DROP TABLE users`)
	return err
}
