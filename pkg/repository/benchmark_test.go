package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	_ "modernc.org/sqlite"
)

// Benchmarks

func BenchmarkFindByID(b *testing.B) {
	db := setupBenchDB(b)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert test data
	_, _ = db.Exec("INSERT INTO users (id, email, name) VALUES ('1', 'test@test.com', 'Test')")

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		_, err := repo.FindByID(ctx, "1")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFindAll_100(b *testing.B) {
	benchmarkFindAll(b, 100)
}

func BenchmarkFindAll_1000(b *testing.B) {
	benchmarkFindAll(b, 1000)
}

func BenchmarkFindAll_10000(b *testing.B) {
	benchmarkFindAll(b, 10000)
}

func benchmarkFindAll(b *testing.B, count int) {
	b.Helper()

	db := setupBenchDB(b)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert test data
	insertBulkUsers(b, db, count)

	b.ResetTimer()
	b.ReportAllocs()

	for range b.N {
		users, err := repo.FindAll(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if len(users) != count {
			b.Fatalf("expected %d users, got %d", count, len(users))
		}
	}
}

func BenchmarkInsert(b *testing.B) {
	db := setupBenchDB(b)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		q, _ := query.Build(
			"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
			map[string]any{
				"id":    fmt.Sprintf("user_%d", i),
				"email": fmt.Sprintf("user%d@test.com", i),
				"name":  fmt.Sprintf("User %d", i),
			},
		)
		_, err := repo.Insert(ctx, q)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUpdate(b *testing.B) {
	db := setupBenchDB(b)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert test data
	_, _ = db.Exec("INSERT INTO users (id, email, name) VALUES ('1', 'test@test.com', 'Test')")

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		q, _ := query.Build(
			"UPDATE users SET name = :name WHERE id = :id",
			map[string]any{"id": "1", "name": fmt.Sprintf("Name %d", i)},
		)
		_, err := repo.Update(ctx, q)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkTransaction(b *testing.B) {
	db := setupBenchDB(b)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := range b.N {
		err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
			q, _ := query.Build(
				"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
				map[string]any{
					"id":    fmt.Sprintf("tx_user_%d", i),
					"email": fmt.Sprintf("tx%d@test.com", i),
					"name":  "TxUser",
				},
			)
			_, err := tx.Insert(ctx, q)
			return err
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Concurrent stress tests
// Note: SQLite has limited concurrency. These tests verify correctness, not throughput.

func setupConcurrentDB(t *testing.T) *sql.DB {
	t.Helper()

	// Use temp file for concurrent tests - memory DB doesn't handle concurrent well
	tmpFile := fmt.Sprintf("/tmp/test_concurrent_%d.db", time.Now().UnixNano())
	t.Cleanup(func() {
		os.Remove(tmpFile)
	})

	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	// SQLite concurrency settings
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	db.SetMaxOpenConns(1) // SQLite works best with single writer

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

func TestConcurrentReads(t *testing.T) {
	db := setupConcurrentDB(t)
	defer db.Close()

	// Insert test data
	for i := range 100 {
		_, _ = db.Exec("INSERT INTO users (id, email, name) VALUES (?, ?, ?)",
			strconv.Itoa(i), fmt.Sprintf("user%d@test.com", i), fmt.Sprintf("User %d", i))
	}

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	const goroutines = 10
	const iterations = 50

	var wg sync.WaitGroup
	var errorCount int64
	var mu sync.Mutex

	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range iterations {
				userID := strconv.Itoa((id*iterations + i) % 100)
				_, err := repo.FindByID(ctx, userID)
				if err != nil && !errors.Is(err, ErrNotFound) {
					mu.Lock()
					errorCount++
					mu.Unlock()
				}
			}
		}(g)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Errorf("concurrent reads had %d errors", errorCount)
	}
}

func TestConcurrentWrites(t *testing.T) {
	db := setupConcurrentDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	const goroutines = 5
	const iterations = 20

	var wg sync.WaitGroup
	var successCount, errorCount int64
	var mu sync.Mutex

	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range iterations {
				q, _ := query.Build(
					"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
					map[string]any{
						"id":    fmt.Sprintf("g%d_i%d", id, i),
						"email": fmt.Sprintf("g%d_i%d@test.com", id, i),
						"name":  fmt.Sprintf("User g%d i%d", id, i),
					},
				)
				_, err := repo.Insert(ctx, q)

				mu.Lock()
				if err != nil {
					errorCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}(g)
	}

	wg.Wait()

	t.Logf("Writes - Success: %d, Errors: %d", successCount, errorCount)

	// Verify data integrity - count should match successes
	count, _ := repo.Count(ctx)
	if count != successCount {
		t.Errorf("count mismatch: got %d rows, expected %d successful inserts", count, successCount)
	}
}

func TestConcurrentTransactions(t *testing.T) {
	db := setupConcurrentDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	const goroutines = 5
	const iterations = 10

	var wg sync.WaitGroup
	var successCount, errorCount int64
	var mu sync.Mutex

	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range iterations {
				err := repo.WithTx(ctx, func(tx *TxRepository[testUser, string]) error {
					q, _ := query.Build(
						"INSERT INTO users (id, email, name) VALUES (:id, :email, :name)",
						map[string]any{
							"id":    fmt.Sprintf("tx_g%d_i%d", id, i),
							"email": fmt.Sprintf("tx_g%d_i%d@test.com", id, i),
							"name":  "TxUser",
						},
					)
					_, err := tx.Insert(ctx, q)
					return err
				})

				mu.Lock()
				if err != nil {
					errorCount++
				} else {
					successCount++
				}
				mu.Unlock()
			}
		}(g)
	}

	wg.Wait()

	t.Logf("Transactions - Success: %d, Errors: %d", successCount, errorCount)

	// Verify data integrity
	count, _ := repo.Count(ctx)
	if count != successCount {
		t.Errorf("count mismatch: got %d, expected %d successful inserts", count, successCount)
	}
}

// Memory stress test

func TestMemoryLargeResultSet(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory test in short mode")
	}

	db := setupTestDB(t)
	defer db.Close()

	repo := New[testUser, string](db, "users")
	ctx := context.Background()

	// Insert 50k rows
	const rowCount = 50000
	t.Logf("Inserting %d rows...", rowCount)

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO users (id, email, name) VALUES (?, ?, ?)")
	defer stmt.Close()
	for i := range rowCount {
		_, _ = stmt.Exec(strconv.Itoa(i), fmt.Sprintf("user%d@test.com", i), fmt.Sprintf("User %d", i))
	}
	_ = tx.Commit()

	t.Log("Fetching all rows...")

	// Fetch all - this tests memory handling
	users, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll failed: %v", err)
	}

	if len(users) != rowCount {
		t.Errorf("expected %d users, got %d", rowCount, len(users))
	}

	t.Logf("Successfully fetched %d rows", len(users))
}

// Helpers

func setupBenchDB(b *testing.B) *sql.DB {
	b.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}

	_, err = db.Exec(`
		CREATE TABLE users (
			id TEXT PRIMARY KEY,
			email TEXT NOT NULL,
			name TEXT NOT NULL
		)
	`)
	if err != nil {
		b.Fatalf("failed to create table: %v", err)
	}

	return db
}

func insertBulkUsers(b *testing.B, db *sql.DB, count int) {
	b.Helper()

	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO users (id, email, name) VALUES (?, ?, ?)")
	defer stmt.Close()
	for i := range count {
		_, _ = stmt.Exec(strconv.Itoa(i), fmt.Sprintf("user%d@test.com", i), fmt.Sprintf("User %d", i))
	}
	_ = tx.Commit()
}
