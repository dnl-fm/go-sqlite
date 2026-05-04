package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/dnl-fm/go-sqlite/pkg/query"
)

func TestWithTx_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
		)
		if err != nil {
			return err
		}

		_, err = tx.Insert(ctx, q)
		if err != nil {
			return err
		}

		// Verify within transaction
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

func TestWithTx_Insert_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		_, err := tx.Insert(ctx, nil)
		if err == nil {
			t.Error("expected error for nil query")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"UPDATE users SET name = :name WHERE id = :id",
			map[string]any{"id": "1", "name": "Alicia"},
		)
		if err != nil {
			return err
		}

		_, err = tx.Update(ctx, q)
		if err != nil {
			return err
		}

		// Verify within transaction
		user, err := tx.FindByID(ctx, "1")
		if err != nil {
			return err
		}
		if user.Name != "Alicia" {
			t.Errorf("Name = %q, want 'Alicia'", user.Name)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Update_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		_, err := tx.Update(ctx, nil)
		if err == nil {
			t.Error("expected error for nil query")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"DELETE FROM users WHERE id = :id",
			map[string]any{"id": "1"},
		)
		if err != nil {
			return err
		}

		_, err = tx.Delete(ctx, q)
		if err != nil {
			return err
		}

		// Verify within transaction
		_, err = tx.FindByID(ctx, "1")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_Delete_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		_, err := tx.Delete(ctx, nil)
		if err == nil {
			t.Error("expected error for nil query")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_DeleteByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		err := tx.DeleteByID(ctx, "1")
		if err != nil {
			return err
		}

		// Verify within transaction
		_, err = tx.FindByID(ctx, "1")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestWithTx_DeleteByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		err := tx.DeleteByID(ctx, "nonexistent")
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}
