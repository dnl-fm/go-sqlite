package scan

import (
	"database/sql"
	"errors"
	"testing"

	_ "turso.tech/database/tursogo"
)

type testUser struct {
	ID    string `db:"id"`
	Email string `db:"email"`
	Name  string `db:"name"`
}

type testPartial struct {
	ID   string `db:"id"`
	Name string `db:"name"`
	// Email not mapped - should be ignored
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := openTursoMemory(t)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	_, err = db.Exec(`INSERT INTO users (id, email, name) VALUES ('1', 'alice@test.com', 'Alice')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	_, err = db.Exec(`INSERT INTO users (id, email, name) VALUES ('2', 'bob@test.com', 'Bob')`)
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	return db
}

func openTursoMemory(t *testing.T) (*sql.DB, error) {
	t.Helper()
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode='mvcc'`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func TestRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users WHERE id = '1'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	user, err := Row[testUser](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if user.ID != "1" {
		t.Errorf("expected ID '1', got '%s'", user.ID)
	}
	if user.Email != "alice@test.com" {
		t.Errorf("expected email 'alice@test.com', got '%s'", user.Email)
	}
	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", user.Name)
	}
}

func TestRow_NoRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users WHERE id = 'nonexistent'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	_, err = Row[testUser](rows)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestAll(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	users, err := All[testUser](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}

	if users[0].ID != "1" || users[0].Name != "Alice" {
		t.Errorf("first user mismatch: %+v", users[0])
	}
	if users[1].ID != "2" || users[1].Name != "Bob" {
		t.Errorf("second user mismatch: %+v", users[1])
	}
}

func TestAll_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users WHERE id = 'nonexistent'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	users, err := All[testUser](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if users == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestOne(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users WHERE id = '1'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	user, err := One[testUser](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.ID != "1" {
		t.Errorf("expected ID '1', got '%s'", user.ID)
	}
}

func TestOne_NoRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	rows, err := db.Query("SELECT * FROM users WHERE id = 'nonexistent'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	user, err := One[testUser](rows)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if user != nil {
		t.Errorf("expected nil, got %+v", user)
	}
}

func TestPartialMapping(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Query all columns but only map some
	rows, err := db.Query("SELECT * FROM users WHERE id = '1'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	user, err := Row[testPartial](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if user.ID != "1" {
		t.Errorf("expected ID '1', got '%s'", user.ID)
	}
	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", user.Name)
	}
}

func TestColumnOrderIndependence(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Query columns in different order than struct
	rows, err := db.Query("SELECT name, id, email FROM users WHERE id = '1'")
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	defer rows.Close()

	user, err := Row[testUser](rows)
	if err != nil {
		t.Fatalf("scan failed: %v", err)
	}

	if user.ID != "1" {
		t.Errorf("expected ID '1', got '%s'", user.ID)
	}
	if user.Email != "alice@test.com" {
		t.Errorf("expected email 'alice@test.com', got '%s'", user.Email)
	}
	if user.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", user.Name)
	}
}
