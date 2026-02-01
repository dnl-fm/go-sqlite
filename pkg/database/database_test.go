package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestOpen(t *testing.T) {
	t.Run("opens in-memory database", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")

		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db == nil {
			t.Fatal("expected non-nil database")
		}

		if db.Path() != ":memory:" {
			t.Errorf("expected path ':memory:', got %s", db.Path())
		}
	})

	t.Run("returns error for empty path", func(t *testing.T) {
		ctx := context.Background()
		_, err := Open(ctx, "")

		if !errors.Is(err, ErrInvalidPath) {
			t.Errorf("expected ErrInvalidPath, got %v", err)
		}
	})

	t.Run("applies custom config", func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig().WithMaxOpenConns(50).WithMaxIdleConns(10)

		db, err := Open(ctx, ":memory:", WithConfig(cfg))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db.Config().MaxOpenConns != 50 {
			t.Errorf("expected MaxOpenConns 50, got %d", db.Config().MaxOpenConns)
		}

		if db.Config().MaxIdleConns != 10 {
			t.Errorf("expected MaxIdleConns 10, got %d", db.Config().MaxIdleConns)
		}
	})

	t.Run("returns error for nil config", func(t *testing.T) {
		ctx := context.Background()
		_, err := Open(ctx, ":memory:", WithConfig(nil))

		if !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("expected ErrInvalidConfig, got %v", err)
		}
	})
}

func TestClose(t *testing.T) {
	t.Run("closes database successfully", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		err = db.Close()
		if err != nil {
			t.Errorf("failed to close database: %v", err)
		}
	})

	t.Run("returns error when closing already closed database", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		db.Close()
		err = db.Close()

		if !errors.Is(err, ErrClosed) {
			t.Errorf("expected ErrClosed, got %v", err)
		}
	})
}

func TestExec(t *testing.T) {
	t.Run("creates table", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		_, err = db.Exec(ctx, `
			CREATE TABLE users (
				id INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				email TEXT UNIQUE
			)
		`)

		if err != nil {
			t.Errorf("failed to create table: %v", err)
		}
	})

	t.Run("inserts data", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

		result, err := db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
		if err != nil {
			t.Fatalf("failed to insert: %v", err)
		}

		id, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("failed to get last insert id: %v", err)
		}

		if id != 1 {
			t.Errorf("expected id 1, got %d", id)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			t.Fatalf("failed to get rows affected: %v", err)
		}

		if affected != 1 {
			t.Errorf("expected 1 row affected, got %d", affected)
		}
	})

	t.Run("updates data", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

		result, err := db.Exec(ctx, "UPDATE users SET name = ? WHERE name = ?", "Bob", "Alice")
		if err != nil {
			t.Fatalf("failed to update: %v", err)
		}

		affected, _ := result.RowsAffected()
		if affected != 1 {
			t.Errorf("expected 1 row affected, got %d", affected)
		}
	})

	t.Run("deletes data", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

		result, err := db.Exec(ctx, "DELETE FROM users WHERE name = ?", "Alice")
		if err != nil {
			t.Fatalf("failed to delete: %v", err)
		}

		affected, _ := result.RowsAffected()
		if affected != 1 {
			t.Errorf("expected 1 row affected, got %d", affected)
		}
	})

	t.Run("returns error when database is closed", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		db.Close()

		_, err = db.Exec(ctx, "SELECT 1")
		if !errors.Is(err, ErrClosed) {
			t.Errorf("expected ErrClosed, got %v", err)
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		cancelCtx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err = db.Exec(cancelCtx, "SELECT 1")
		if err == nil {
			t.Error("expected error from cancelled context")
		}
	})
}

func TestQuery(t *testing.T) {
	t.Run("queries multiple rows", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Bob")
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Charlie")

		rows, err := db.Query(ctx, "SELECT id, name FROM users ORDER BY id")
		if err != nil {
			t.Fatalf("failed to query: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			var id int
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				t.Fatalf("failed to scan: %v", err)
			}
			count++
		}

		if err := rows.Err(); err != nil {
			t.Fatalf("rows error: %v", err)
		}

		if count != 3 {
			t.Errorf("expected 3 rows, got %d", count)
		}
	})

	t.Run("returns error when database is closed", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		db.Close()

		_, err = db.Query(ctx, "SELECT 1")
		if !errors.Is(err, ErrClosed) {
			t.Errorf("expected ErrClosed, got %v", err)
		}
	})
}

func TestQueryOne(t *testing.T) {
	t.Run("queries single row", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
		db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

		row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "Alice")

		var id int
		var name string
		if err := row.Scan(&id, &name); err != nil {
			t.Fatalf("failed to scan: %v", err)
		}

		if id != 1 {
			t.Errorf("expected id 1, got %d", id)
		}

		if name != "Alice" {
			t.Errorf("expected name Alice, got %s", name)
		}
	})

	t.Run("returns ErrNoRows when no row found", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

		row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "NonExistent")

		var id int
		var name string
		err = row.Scan(&id, &name)

		if !errors.Is(err, sql.ErrNoRows) {
			t.Errorf("expected sql.ErrNoRows, got %v", err)
		}
	})
}

func TestBeginTx(t *testing.T) {
	t.Run("executes transaction successfully", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}

		_, err = tx.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
		if err != nil {
			t.Fatalf("failed to insert in transaction: %v", err)
		}

		if err := tx.Commit(); err != nil {
			t.Fatalf("failed to commit transaction: %v", err)
		}

		var count int
		db.QueryOne(ctx, "SELECT COUNT(*) FROM users").Scan(&count)

		if count != 1 {
			t.Errorf("expected 1 row after commit, got %d", count)
		}
	})

	t.Run("rolls back transaction", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("failed to begin transaction: %v", err)
		}

		_, err = tx.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
		if err != nil {
			t.Fatalf("failed to insert in transaction: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("failed to rollback transaction: %v", err)
		}

		var count int
		db.QueryOne(ctx, "SELECT COUNT(*) FROM users").Scan(&count)

		if count != 0 {
			t.Errorf("expected 0 rows after rollback, got %d", count)
		}
	})

	t.Run("returns error when database is closed", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:")
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}

		db.Close()

		_, err = db.BeginTx(ctx, nil)
		if !errors.Is(err, ErrClosed) {
			t.Errorf("expected ErrClosed, got %v", err)
		}
	})
}

func TestConfig(t *testing.T) {
	t.Run("DefaultConfig has expected values", func(t *testing.T) {
		cfg := DefaultConfig()

		if cfg.MaxOpenConns != 25 {
			t.Errorf("expected MaxOpenConns 25, got %d", cfg.MaxOpenConns)
		}

		if cfg.MaxIdleConns != 5 {
			t.Errorf("expected MaxIdleConns 5, got %d", cfg.MaxIdleConns)
		}

		if cfg.ConnMaxLifetime != 5*time.Minute {
			t.Errorf("expected ConnMaxLifetime 5m, got %v", cfg.ConnMaxLifetime)
		}

		if cfg.Pragmas["journal_mode"] != "WAL" {
			t.Errorf("expected journal_mode WAL, got %s", cfg.Pragmas["journal_mode"])
		}
	})

	t.Run("DevelopmentConfig has expected values", func(t *testing.T) {
		cfg := DevelopmentConfig()

		if cfg.MaxOpenConns != 10 {
			t.Errorf("expected MaxOpenConns 10, got %d", cfg.MaxOpenConns)
		}

		if cfg.Pragmas["journal_mode"] != "DELETE" {
			t.Errorf("expected journal_mode DELETE, got %s", cfg.Pragmas["journal_mode"])
		}
	})

	t.Run("ProductionConfig has expected values", func(t *testing.T) {
		cfg := ProductionConfig()

		if cfg.MaxOpenConns != 100 {
			t.Errorf("expected MaxOpenConns 100, got %d", cfg.MaxOpenConns)
		}

		if cfg.Pragmas["foreign_keys"] != "ON" {
			t.Errorf("expected foreign_keys ON, got %s", cfg.Pragmas["foreign_keys"])
		}
	})

	t.Run("WithMaxOpenConns chains correctly", func(t *testing.T) {
		cfg := DefaultConfig().WithMaxOpenConns(50)

		if cfg.MaxOpenConns != 50 {
			t.Errorf("expected MaxOpenConns 50, got %d", cfg.MaxOpenConns)
		}
	})

	t.Run("WithMaxIdleConns chains correctly", func(t *testing.T) {
		cfg := DefaultConfig().WithMaxIdleConns(15)

		if cfg.MaxIdleConns != 15 {
			t.Errorf("expected MaxIdleConns 15, got %d", cfg.MaxIdleConns)
		}
	})

	t.Run("WithConnMaxLifetime chains correctly", func(t *testing.T) {
		cfg := DefaultConfig().WithConnMaxLifetime(10 * time.Minute)

		if cfg.ConnMaxLifetime != 10*time.Minute {
			t.Errorf("expected ConnMaxLifetime 10m, got %v", cfg.ConnMaxLifetime)
		}
	})

	t.Run("WithPragma chains correctly", func(t *testing.T) {
		cfg := DefaultConfig().WithPragma("temp_store", "MEMORY")

		if cfg.Pragmas["temp_store"] != "MEMORY" {
			t.Errorf("expected temp_store MEMORY, got %s", cfg.Pragmas["temp_store"])
		}
	})

	t.Run("methods chain together", func(t *testing.T) {
		cfg := DefaultConfig().
			WithMaxOpenConns(75).
			WithMaxIdleConns(20).
			WithConnMaxLifetime(8 * time.Minute).
			WithPragma("temp_store", "MEMORY")

		if cfg.MaxOpenConns != 75 {
			t.Errorf("expected MaxOpenConns 75, got %d", cfg.MaxOpenConns)
		}

		if cfg.MaxIdleConns != 20 {
			t.Errorf("expected MaxIdleConns 20, got %d", cfg.MaxIdleConns)
		}

		if cfg.ConnMaxLifetime != 8*time.Minute {
			t.Errorf("expected ConnMaxLifetime 8m, got %v", cfg.ConnMaxLifetime)
		}

		if cfg.Pragmas["temp_store"] != "MEMORY" {
			t.Errorf("expected temp_store MEMORY, got %s", cfg.Pragmas["temp_store"])
		}
	})
}

func TestConnectionPool(t *testing.T) {
	t.Run("applies connection pool settings", func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig().
			WithMaxOpenConns(30).
			WithMaxIdleConns(8).
			WithConnMaxLifetime(3 * time.Minute)

		db, err := Open(ctx, ":memory:", WithConfig(cfg))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		stats := db.DB().Stats()

		if stats.MaxOpenConnections != 30 {
			t.Errorf("expected MaxOpenConnections 30, got %d", stats.MaxOpenConnections)
		}
	})
}

func TestErrors(t *testing.T) {
	t.Run("QueryError wraps error correctly", func(t *testing.T) {
		baseErr := errors.New("syntax error")
		qErr := &QueryError{Query: "SELECT * FROM invalid", Err: baseErr}

		if !errors.Is(qErr, baseErr) {
			t.Error("QueryError should unwrap to base error")
		}

		errMsg := qErr.Error()
		if errMsg == "" {
			t.Error("QueryError should have non-empty error message")
		}
	})

	t.Run("ExecError wraps error correctly", func(t *testing.T) {
		baseErr := errors.New("constraint violation")
		eErr := &ExecError{Query: "INSERT INTO users", Err: baseErr}

		if !errors.Is(eErr, baseErr) {
			t.Error("ExecError should unwrap to base error")
		}

		errMsg := eErr.Error()
		if errMsg == "" {
			t.Error("ExecError should have non-empty error message")
		}
	})
}
