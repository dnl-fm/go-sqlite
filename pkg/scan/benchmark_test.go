package scan

import (
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/tursodatabase/turso-go"
)

// Benchmarks for scan package

func BenchmarkRow(b *testing.B) {
	db := setupBenchDB(b, 1)
	defer db.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users WHERE id = '0'")
		_, err := Row[testUser](rows)
		rows.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkAll_100(b *testing.B) {
	benchmarkAll(b, 100)
}

func BenchmarkAll_1000(b *testing.B) {
	benchmarkAll(b, 1000)
}

func BenchmarkAll_10000(b *testing.B) {
	benchmarkAll(b, 10000)
}

func benchmarkAll(b *testing.B, count int) {
	db := setupBenchDB(b, count)
	defer db.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users")
		users, err := All[testUser](rows)
		rows.Close()
		if err != nil {
			b.Fatal(err)
		}
		if len(users) != count {
			b.Fatalf("expected %d, got %d", count, len(users))
		}
	}
}

func BenchmarkOne(b *testing.B) {
	db := setupBenchDB(b, 1)
	defer db.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users WHERE id = '0'")
		_, err := One[testUser](rows)
		rows.Close()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Compare manual scan vs automatic scan

func BenchmarkManualScan(b *testing.B) {
	db := setupBenchDB(b, 100)
	defer db.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users")
		var users []testUser
		for rows.Next() {
			var u testUser
			rows.Scan(&u.ID, &u.Email, &u.Name)
			users = append(users, u)
		}
		rows.Close()
		if len(users) != 100 {
			b.Fatalf("expected 100, got %d", len(users))
		}
	}
}

func BenchmarkAutoScan(b *testing.B) {
	db := setupBenchDB(b, 100)
	defer db.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users")
		users, err := All[testUser](rows)
		rows.Close()
		if err != nil {
			b.Fatal(err)
		}
		if len(users) != 100 {
			b.Fatalf("expected 100, got %d", len(users))
		}
	}
}

// Test struct field cache effectiveness

func BenchmarkFieldCacheHit(b *testing.B) {
	db := setupBenchDB(b, 1)
	defer db.Close()

	// Warm up cache
	rows, _ := db.Query("SELECT * FROM users")
	Row[testUser](rows)
	rows.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rows, _ := db.Query("SELECT * FROM users")
		Row[testUser](rows)
		rows.Close()
	}
}

// Helpers

func setupBenchDB(b *testing.B, rowCount int) *sql.DB {
	b.Helper()

	db, err := sql.Open("turso", ":memory:")
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

	// Bulk insert
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare("INSERT INTO users (id, email, name) VALUES (?, ?, ?)")
	for i := 0; i < rowCount; i++ {
		stmt.Exec(fmt.Sprintf("%d", i), fmt.Sprintf("user%d@test.com", i), fmt.Sprintf("User %d", i))
	}
	stmt.Close()
	tx.Commit()

	return db
}
