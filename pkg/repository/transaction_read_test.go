package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/dnl-fm/go-sqlite/pkg/query"
)

func TestWithTx_FindByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		user, err := tx.FindByID(ctx, "1")
		if err != nil {
			return err
		}
		if user.Name != "Alice" {
			t.Errorf("Name = %q, want 'Alice'", user.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		_, err := tx.FindByID(ctx, "nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindAll(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		users, err := tx.FindAll(ctx)
		if err != nil {
			return err
		}
		if len(users) != 2 {
			t.Errorf("len(users) = %d, want 2", len(users))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"SELECT * FROM users WHERE name = :name",
			map[string]any{"name": "Alice"},
		)
		if err != nil {
			return err
		}

		users, err := tx.FindByQuery(ctx, q)
		if err != nil {
			return err
		}
		if len(users) != 1 {
			t.Errorf("len(users) = %d, want 1", len(users))
		}
		if users[0].Email != "alice@test.com" {
			t.Errorf("Email = %q, want 'alice@test.com'", users[0].Email)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindByQuery_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		_, err := tx.FindByQuery(ctx, nil)
		if err == nil {
			t.Error("expected error for nil query")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindOneByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"SELECT * FROM users WHERE id = :id",
			map[string]any{"id": "1"},
		)
		if err != nil {
			return err
		}

		user, err := tx.FindOneByQuery(ctx, q)
		if err != nil {
			return err
		}
		if user == nil {
			t.Error("expected user, got nil")
			return nil
		}
		if user.Name != "Alice" {
			t.Errorf("Name = %q, want 'Alice'", user.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_FindOneByQuery_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"SELECT * FROM users WHERE id = :id",
			map[string]any{"id": "nonexistent"},
		)
		if err != nil {
			return err
		}

		user, err := tx.FindOneByQuery(ctx, q)
		if err != nil {
			return err
		}
		if user != nil {
			t.Errorf("expected nil, got %+v", user)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Count(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		count, err := tx.Count(ctx)
		if err != nil {
			return err
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Exists(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		exists, err := tx.Exists(ctx, "1")
		if err != nil {
			return err
		}
		if !exists {
			t.Error("expected exists=true")
		}

		exists, err = tx.Exists(ctx, "nonexistent")
		if err != nil {
			return err
		}
		if exists {
			t.Error("expected exists=false")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}
