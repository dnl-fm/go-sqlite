package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	_ "github.com/tursodatabase/turso-go"
)

func TestWithTx_Commit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert within transaction
	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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
	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound (rollback), got %v", err)
	}
}

func TestWithTx_NilDB(t *testing.T) {
	repo := New[testUser, string](nil, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
		return nil
	})

	if err != ErrNilDB {
		t.Errorf("expected ErrNilDB, got %v", err)
	}
}

func TestTxRepository_FindByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_FindByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
		_, err := tx.FindByID(ctx, "nonexistent")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestTxRepository_FindAll(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_FindByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_FindByQuery_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_FindOneByQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_FindOneByQuery_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Count(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")
	insertTestUser(t, db, "2", "bob@test.com", "Bob")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Exists(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Insert(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Insert_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Update_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestTxRepository_Delete_NilQuery(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_DeleteByID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
		err := tx.DeleteByID(ctx, "1")
		if err != nil {
			return err
		}

		// Verify within transaction
		_, err = tx.FindByID(ctx, "1")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestTxRepository_DeleteByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
		err := tx.DeleteByID(ctx, "nonexistent")
		if err != ErrNotFound {
			t.Errorf("expected ErrNotFound, got %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestTxRepository_Tx(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
		if tx.Tx() == nil {
			t.Error("Tx() should not return nil")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithTx failed: %v", err)
	}
}

func TestTxRepository_MultipleOperations(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Multiple operations in single transaction
	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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

func TestTxRepository_IsolationFromOutside(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	insertTestUser(t, db, "1", "alice@test.com", "Alice")

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Start transaction but don't commit yet
	err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
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
