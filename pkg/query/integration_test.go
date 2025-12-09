package query

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/tursodatabase/turso-go"
)

func TestIntegration_Insert(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	q, err := Build(
		"INSERT INTO users (name, email) VALUES (:name, :email)",
		map[string]any{"name": "Alice", "email": "alice@example.com"},
	)
	if err != nil {
		t.Fatalf("failed to build query: %v", err)
	}

	result, err := db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", rowsAffected)
	}
}

func TestIntegration_Select(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = db.ExecContext(ctx, "INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	q, err := Build(
		"SELECT name, email FROM users WHERE name = :name",
		map[string]any{"name": "Alice"},
	)
	if err != nil {
		t.Fatalf("failed to build query: %v", err)
	}

	var name, email string
	err = db.QueryRowContext(ctx, q.SQL(), q.Args()...).Scan(&name, &email)
	if err != nil {
		t.Fatalf("failed to query row: %v", err)
	}

	if name != "Alice" {
		t.Errorf("expected name 'Alice', got %q", name)
	}
	if email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got %q", email)
	}
}

func TestIntegration_Update(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			email TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = db.ExecContext(ctx, "INSERT INTO users (name, email) VALUES ('Alice', 'alice@example.com')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	q, err := Build(
		"UPDATE users SET email = :email WHERE name = :name",
		map[string]any{"name": "Alice", "email": "alice.new@example.com"},
	)
	if err != nil {
		t.Fatalf("failed to build query: %v", err)
	}

	result, err := db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	// Verify update
	var email string
	db.QueryRowContext(ctx, "SELECT email FROM users WHERE name = 'Alice'").Scan(&email)
	if email != "alice.new@example.com" {
		t.Errorf("expected updated email, got %q", email)
	}
}

func TestIntegration_Delete(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = db.ExecContext(ctx, "INSERT INTO users (name) VALUES ('Alice'), ('Bob')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	q, err := Build(
		"DELETE FROM users WHERE name = :name",
		map[string]any{"name": "Alice"},
	)
	if err != nil {
		t.Fatalf("failed to build query: %v", err)
	}

	result, err := db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		t.Fatalf("failed to execute query: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	// Verify delete
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 remaining row, got %d", count)
	}
}

func TestIntegration_DuplicatePlaceholders(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE products (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			category TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = db.ExecContext(ctx, "INSERT INTO products (name, category) VALUES ('Laptop', 'Electronics'), ('Phone', 'Electronics')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Query with same placeholder used twice
	q, err := Build(
		"SELECT * FROM products WHERE category = :search OR name LIKE :search",
		map[string]any{"search": "Electronics"},
	)
	if err != nil {
		t.Fatalf("failed to build query: %v", err)
	}

	rows, err := db.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}

	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func TestIntegration_New(t *testing.T) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	// Insert test data
	_, err = db.ExecContext(ctx, "INSERT INTO users (name) VALUES ('Alice'), ('Bob')")
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	q, err := New("SELECT * FROM users")
	if err != nil {
		t.Fatalf("failed to create query: %v", err)
	}

	rows, err := db.QueryContext(ctx, q.SQL())
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
	}

	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

func BenchmarkBuild(b *testing.B) {
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		b.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	db.ExecContext(ctx, `CREATE TABLE bench (id INTEGER PRIMARY KEY, name TEXT, value INTEGER)`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q, _ := Build(
			"INSERT INTO bench (name, value) VALUES (:name, :value)",
			map[string]any{"name": "test", "value": i},
		)
		db.ExecContext(ctx, q.SQL(), q.Args()...)
	}
}
