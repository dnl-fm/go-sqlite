package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func rejectWithoutRowIDSQL(query string) error {
	normalized := strings.Join(strings.Fields(strings.ToUpper(query)), " ")
	if strings.Contains(normalized, "WITHOUT ROWID") {
		return errors.New("go-sqlite requires rowid tables: WITHOUT ROWID is not supported")
	}
	return nil
}

// ValidateRowIDSchema rejects databases that contain WITHOUT ROWID tables.
func ValidateRowIDSchema(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT name, sql
		FROM sqlite_schema
		WHERE type = 'table'
		  AND sql IS NOT NULL
	`)
	if err != nil {
		return fmt.Errorf("inspect schema for rowid compatibility: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var createSQL string
		if err := rows.Scan(&name, &createSQL); err != nil {
			return fmt.Errorf("scan schema row: %w", err)
		}
		if err := rejectWithoutRowIDSQL(createSQL); err != nil {
			return fmt.Errorf("table %s: %w", name, err)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate schema rows: %w", err)
	}
	return nil
}
