package repository

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dnl-fm/go-sqlite/pkg/database"
	"github.com/dnl-fm/go-sqlite/pkg/query"
)

func TestWithConcurrentTx_Commit(t *testing.T) {
	db := setupTursoMVCCRepositoryDB(t)
	defer db.Close()

	repo := New[testUser, string](db.DB(), "users")
	ctx := context.Background()

	err := repo.WithConcurrentTx(ctx, func(tx *Repository[testUser, string]) error {
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
		t.Fatalf("WithConcurrentTx failed: %v", err)
	}

	user, err := repo.FindByID(ctx, "1")
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if user.Name != "Alice" {
		t.Errorf("Name = %q, want 'Alice'", user.Name)
	}
}

func TestWithConcurrentTx_Rollback(t *testing.T) {
	db := setupTursoMVCCRepositoryDB(t)
	defer db.Close()

	repo := New[testUser, string](db.DB(), "users")
	ctx := context.Background()

	err := repo.WithConcurrentTx(ctx, func(tx *Repository[testUser, string]) error {
		q, err := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{"id": "1", "email": "alice@test.com", "name": "Alice"},
		)
		if err != nil {
			return err
		}
		if _, err := tx.Insert(ctx, q); err != nil {
			return err
		}
		return errors.New("intentional rollback")
	})
	if err == nil {
		t.Fatal("expected rollback error, got nil")
	}

	_, err = repo.FindByID(ctx, "1")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after rollback, got %v", err)
	}
}

func TestWithConcurrentTx_AcrossRepositoryHandles(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/shared-repository.db"

	setupDB, openErr := database.Open(ctx, path, database.WithTursoMVCC())
	if openErr != nil {
		t.Fatalf("failed to open setup database: %v", openErr)
	}
	if _, execErr := setupDB.Exec(ctx, `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			name TEXT NOT NULL
		)
	`); execErr != nil {
		t.Fatalf("failed to create users table: %v", execErr)
	}
	if closeErr := setupDB.Close(); closeErr != nil {
		t.Fatalf("failed to close setup database: %v", closeErr)
	}

	const handles = 4
	const workersPerHandle = 8
	dbs := make([]*database.Database, 0, handles)
	repos := make([]*Repository[testUser, string], 0, handles)
	for handle := range handles {
		dbHandle, handleErr := database.Open(ctx, path, database.WithTursoMVCC())
		if handleErr != nil {
			t.Fatalf("failed to open database handle %d: %v", handle, handleErr)
		}
		dbs = append(dbs, dbHandle)
		repos = append(repos, New[testUser, string](dbHandle.DB(), "users"))
	}
	defer func() {
		for _, dbHandle := range dbs {
			if closeErr := dbHandle.Close(); closeErr != nil {
				t.Errorf("failed to close database handle: %v", closeErr)
			}
		}
	}()

	start := make(chan struct{})
	commit := make(chan struct{})
	errs := make(chan error, handles*workersPerHandle)

	var wg sync.WaitGroup
	for handle, repo := range repos {
		for worker := range workersPerHandle {
			id := handle*workersPerHandle + worker
			wg.Add(1)
			go func(repo *Repository[testUser, string], id int) {
				defer wg.Done()
				<-start
				txErr := repo.WithConcurrentTx(ctx, func(tx *Repository[testUser, string]) error {
					q, buildErr := query.Build(
						"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
						map[string]any{
							"id":    fmt.Sprintf("user_%02d", id),
							"email": fmt.Sprintf("user_%02d@test.com", id),
							"name":  fmt.Sprintf("User %02d", id),
						},
					)
					if buildErr != nil {
						return buildErr
					}
					_, insertErr := tx.Insert(ctx, q)
					if insertErr != nil {
						return insertErr
					}
					<-commit
					return nil
				})
				if txErr != nil {
					errs <- txErr
				}
			}(repo, id)
		}
	}

	time.Sleep(50 * time.Millisecond)
	close(start)
	time.Sleep(100 * time.Millisecond)
	close(commit)
	wg.Wait()
	close(errs)

	var failures []error
	for err := range errs {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		t.Fatalf("expected cross-handle repository writes to succeed, got %d failures; first: %v", len(failures), failures[0])
	}

	count, err := repos[0].Count(ctx)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if want := int64(handles * workersPerHandle); count != want {
		t.Fatalf("expected %d rows, got %d", want, count)
	}
}

func setupTursoMVCCRepositoryDB(t *testing.T) *database.Database {
	t.Helper()

	db, err := database.Open(context.Background(), t.TempDir()+"/repository.db", database.WithTursoMVCC())
	if err != nil {
		t.Fatalf("failed to open turso MVCC database: %v", err)
	}

	_, err = db.Exec(context.Background(), `
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("failed to create users table: %v", err)
	}

	return db
}
