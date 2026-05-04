package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestQueryMultipleRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Bob")
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Charlie")

	rows, err := db.Query(ctx, "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		if err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		count++
	}

	err = rows.Err()
	if err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

func TestQueryClosed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	db.Close()

	rows, err := db.Query(ctx, "SELECT 1")
	if rows != nil {
		_ = rows.Err()
		rows.Close()
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestQueryOneSingleRow(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "Alice")

	var id int
	var name string
	err = row.Scan(&id, &name)
	if err != nil {
		t.Fatalf("failed to scan: %v", err)
	}

	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	if name != "Alice" {
		t.Errorf("expected name Alice, got %s", name)
	}
}

func TestQueryOneNoRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

	row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "NonExistent")

	var id int
	var name string
	err = row.Scan(&id, &name)

	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}
