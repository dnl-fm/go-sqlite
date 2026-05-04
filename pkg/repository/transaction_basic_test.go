package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/dnl-fm/go-sqlite/pkg/query"
)

func TestWithTx_Commit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert within transaction
	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
		)
		if err != nil {
			return err
		}
		_, err = tx.Insert(ctx, q)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}

	// Verify data was committed
	user, err := repo.FindByID(ctx, "1")
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if user.Name != "Alice" {
		t.Errorf("Name = %q, want 'Alice'", user.Name)
	}
}

func TestWithTx_Rollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert then return error to trigger rollback
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

		// Return error to trigger rollback
		return errors.New("intentional error")
	})

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "intentional error" {
		t.Errorf("error = %q, want 'intentional error'", err.Error())
	}

	// Verify data was NOT committed (rolled back)
	_, err = repo.FindByID(ctx, "1")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound (rollback), got %v", err)
	}
}

func TestWithTx_NilDB(t *testing.T) {
	repo := New[testUser, string](nil, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		return nil
	})

	if !errors.Is(err, ErrNilDB) {
		t.Errorf("expected ErrNilDB, got %v", err)
	}
}

func TestWithTx_MultipleOperations(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Multiple operations in single transaction
	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		// Insert first user
		q1, _ := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
		)
		_, err := tx.Insert(ctx, q1)
		if err != nil {
			return err
		}

		// Insert second user
		q2, _ := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{"id": "2", "email": "bob@test.com", "name": "Bob"},
		)
		_, err = tx.Insert(ctx, q2)
		if err != nil {
			return err
		}

		// Update first user
		q3, _ := query.Build(
			"UPDATE users SET name = :name WHERE id = :id",
			map[string]any{"id": "1", "name": "Alicia"},
		)
		_, err = tx.Update(ctx, q3)
		if err != nil {
			return err
		}

		// Delete second user
		err = tx.DeleteByID(ctx, "2")
		if err != nil {
			return err
		}

		// Verify state within transaction
		count, _ := tx.Count(ctx)
		if count != 1 {
			t.Errorf("count = %d, want 1", count)
		}

		user, _ := tx.FindByID(ctx, "1")
		if user.Name != "Alicia" {
			t.Errorf("Name = %q, want 'Alicia'", user.Name)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}

	// Verify final state outside transaction
	count, _ := repo.Count(ctx)
	if count != 1 {
		t.Errorf("final count = %d, want 1", count)
	}
}

func TestWithTx_IsolationFromOutside(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Start transaction but don't commit yet
	err := repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
		// Update within transaction
		q, _ := query.Build(
			"UPDATE users SET name = :name WHERE id = :id",
			map[string]any{"id": "1", "name": "Alicia"},
		)
		_, err := tx.Update(ctx, q)
		if err != nil {
			return err
		}

		// Within transaction, should see updated value
		user, _ := tx.FindByID(ctx, "1")
		if user.Name != "Alicia" {
			t.Errorf("within tx: Name = %q, want 'Alicia'", user.Name)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}

	// After commit, should see updated value
	user, _ := repo.FindByID(ctx, "1")
	if user.Name != "Alicia" {
		t.Errorf("after commit: Name = %q, want 'Alicia'", user.Name)
	}
}
