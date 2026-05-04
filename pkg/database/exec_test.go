package database

import (
	"context"
	"errors"
	"testing"
)

func TestExecCreateTable(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT UNIQUE
		)
	`)

	if err != nil {
		t.Errorf("failed to create table: %v", err)
	}
}

func TestExecInsert(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

	result, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("failed to get last insert id: %v", err)
	}

	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("failed to get rows affected: %v", err)
	}

	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestExecUpdate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	result, err := db.Exec(ctx, "UPDATE users SET name = ? WHERE name = ?", "Bob", "Alice")
	if err != nil {
		t.Fatalf("failed to update: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestExecDelete(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	result, err := db.Exec(ctx, "DELETE FROM users WHERE name = ?", "Alice")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestExecClosed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	db.Close()

	_, err = db.Exec(ctx, "SELECT 1")
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestExecContextCancellation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = db.Exec(cancelCtx, "SELECT 1")
	if err == nil {
		t.Error("expected error from canceled context")
	}
}
