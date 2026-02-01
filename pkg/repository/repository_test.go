package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	_ "turso.tech/database/tursogo"
)

type testUser struct {
	ID    string `db:"id"`
	Email string `db:"email"`
	Name  string `db:"name"`
}

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("turso", ":memory:")
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

	return db
}

func insertTestUser(t *testing.T, db *sql.DB, id, email, name string) {
	t.Helper()
	_, err := db.Exec("INSERT INTO users (id, email, name) VALUES (?, ?, ?)", id, email, name)
	if err != nil {
		t.Fatalf("failed to insert test user: %v", err)
	}
}

func TestNew(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")

	if repo.DB() != db {
		t.Error("DB() should return the database")
	}
	if repo.TableName() != "users" {
		t.Errorf("TableName() = %q, want %q", repo.TableName(), "users")
	}
}

func TestFindByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	user, err := repo.FindByID(ctx, "1")
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}

	if user.ID != "1" {
		t.Errorf("ID = %q, want %q", user.ID, "1")
	}
	if user.Email != "alice@test.com" {
		t.Errorf("Email = %q, want %q", user.Email, "alice@test.com")
	}
	if user.Name != "Alice" {
		t.Errorf("Name = %q, want %q", user.Name, "Alice")
	}
}

func TestFindByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestFindAll(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	users, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
}

func TestFindAll_Empty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	users, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if users == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestFindByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	q, err := query.Build(
		"SELECT * FROM users WHERE email = :email",
		map[string]any{"email": "alice@test.com"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	users, err := repo.FindByQuery(ctx, q)
	if err != nil {
		t.Fatalf("FindByQuery failed: %v", err)
	}

	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Email != "alice@test.com" {
		t.Errorf("Email = %q, want %q", users[0].Email, "alice@test.com")
	}
}

func TestFindOneByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	q, err := query.Build(
		"SELECT * FROM users WHERE id = :id",
		map[string]any{"id": "1"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	user, err := repo.FindOneByQuery(ctx, q)
	if err != nil {
		t.Fatalf("FindOneByQuery failed: %v", err)
	}

	if user == nil {
		t.Fatal("expected user, got nil")
	}
	if user.ID != "1" {
		t.Errorf("ID = %q, want %q", user.ID, "1")
	}
}

func TestFindOneByQuery_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	q, err := query.Build(
		"SELECT * FROM users WHERE id = :id",
		map[string]any{"id": "nonexistent"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	user, err := repo.FindOneByQuery(ctx, q)
	if err != nil {
		t.Fatalf("FindOneByQuery failed: %v", err)
	}

	if user != nil {
		t.Errorf("expected nil, got %+v", user)
	}
}

func TestCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	count, err := repo.Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 2 {
		t.Errorf("Count = %d, want %d", count, 2)
	}
}

func TestExists(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	exists, err := repo.Exists(ctx, "1")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected exists=true")
	}

	exists, err = repo.Exists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected exists=false")
	}
}

func TestInsert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	q, err := query.Build(
		"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
		map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	result, err := repo.Insert(ctx, q)
	if err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("RowsAffected = %d, want 1", rowsAffected)
	}

	// Verify insert
	user, err := repo.FindByID(ctx, "1")
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if user.Email != "alice@test.com" {
		t.Errorf("Email = %q, want %q", user.Email, "alice@test.com")
	}
}

func TestUpdate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	q, err := query.Build(
		"UPDATE users SET name = :name WHERE id = :id",
		map[string]any{"id": "1", "name": "Alice Smith"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	result, err := repo.Update(ctx, q)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		t.Errorf("RowsAffected = %d, want 1", rowsAffected)
	}

	// Verify update
	user, err := repo.FindByID(ctx, "1")
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if user.Name != "Alice Smith" {
		t.Errorf("Name = %q, want %q", user.Name, "Alice Smith")
	}
}

func TestDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Verify 2 users before delete
	count, _ := repo.Count(ctx)
	if count != 2 {
		t.Fatalf("expected 2 users before delete, got %d", count)
	}

	q, err := query.Build(
		"DELETE FROM users WHERE id = :id",
		map[string]any{"id": "1"},
	)
	if err != nil {
		t.Fatalf("query.Build failed: %v", err)
	}

	_, err = repo.Delete(ctx, q)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify delete - only user 1 should be gone
	_, err = repo.FindByID(ctx, "1")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound for user 1, got %v", err)
	}

	// User 2 should still exist
	user2, err := repo.FindByID(ctx, "2")
	if err != nil {
		t.Errorf("user 2 should still exist, got error: %v", err)
	}
	if user2.Name != "Bob" {
		t.Errorf("user 2 name = %q, want 'Bob'", user2.Name)
	}

	// Count should be 1
	count, _ = repo.Count(ctx)
	if count != 1 {
		t.Errorf("expected 1 user after delete, got %d", count)
	}
}

func TestDeleteByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.DeleteByID(ctx, "1")
	if err != nil {
		t.Fatalf("DeleteByID failed: %v", err)
	}

	// Verify delete
	_, err = repo.FindByID(ctx, "1")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestDeleteByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.DeleteByID(ctx, "nonexistent")
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestNilDB(t *testing.T) {
	repo := New[testUser, string](nil, "users")
	ctx := context.Background()

	_, err := repo.FindByID(ctx, "1")
	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}

	_, err = repo.FindAll(ctx)
	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}

	_, err = repo.Count(ctx)
	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}

	_, err = repo.Exists(ctx, "1")
	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}

	err = repo.DeleteByID(ctx, "1")
	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}
}
