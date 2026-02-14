package database

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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

func TestWithDriver(t *testing.T) {
	t.Run("sets driver name", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:", WithDriver("sqlite"))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db.Config().Driver != "sqlite" {
			t.Errorf("expected driver 'sqlite', got %s", db.Config().Driver)
		}
	})

	t.Run("empty driver name keeps default", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:", WithDriver(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer db.Close()

		if db.Config().Driver != DefaultDriver {
			t.Errorf("expected default driver %s, got %s", DefaultDriver, db.Config().Driver)
		}
	})

	t.Run("overrides config driver", func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig().WithDriver("other")
		db, err := Open(ctx, ":memory:", WithConfig(cfg), WithDriver("sqlite"))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db.Config().Driver != "sqlite" {
			t.Errorf("expected driver 'sqlite' after override, got %s", db.Config().Driver)
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

func TestExecCreateTable(t *testing.T) {
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
}

func TestExecInsert(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

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
}

func TestExecUpdate(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	result, err := db.Exec(ctx, "UPDATE users SET name = ? WHERE name = ?", "Bob", "Alice")
	if err != nil {
		t.Fatalf("failed to update: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestExecDelete(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	result, err := db.Exec(ctx, "DELETE FROM users WHERE name = ?", "Alice")
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	affected, _ := result.RowsAffected()
	if affected != 1 {
		t.Errorf("expected 1 row affected, got %d", affected)
	}
}

func TestExecClosed(t *testing.T) {
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
}

func TestExecContextCancellation(t *testing.T) {
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
		t.Error("expected error from canceled context")
	}
}

func TestQueryMultipleRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Bob")
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Charlie")

	rows, err := db.Query(ctx, "SELECT id, name FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("failed to query: %v", err)
	}
	defer rows.Close()

	var count int
	for rows.Next() {
		var id int
		var name string
		err = rows.Scan(&id, &name)
		if err != nil {
			t.Fatalf("failed to scan: %v", err)
		}
		count++
	}

	err = rows.Err()
	if err != nil {
		t.Fatalf("rows error: %v", err)
	}

	if count != 3 {
		t.Errorf("expected 3 rows, got %d", count)
	}
}

func TestQueryClosed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}

	db.Close()

	rows, err := db.Query(ctx, "SELECT 1")
	if rows != nil {
		_ = rows.Err()
		rows.Close()
	}
	if !errors.Is(err, ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestQueryOneSingleRow(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)
	_, _ = db.Exec(ctx, "INSERT INTO users (name) VALUES (?)", "Alice")

	row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "Alice")

	var id int
	var name string
	err = row.Scan(&id, &name)
	if err != nil {
		t.Fatalf("failed to scan: %v", err)
	}

	if id != 1 {
		t.Errorf("expected id 1, got %d", id)
	}

	if name != "Alice" {
		t.Errorf("expected name Alice, got %s", name)
	}
}

func TestQueryOneNoRows(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

	row := db.QueryOne(ctx, "SELECT id, name FROM users WHERE name = ?", "NonExistent")

	var id int
	var name string
	err = row.Scan(&id, &name)

	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

func TestBeginTxCommit(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	_, err = tx.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		t.Fatalf("failed to insert in transaction: %v", err)
	}

	err = tx.Commit()
	if err != nil {
		t.Fatalf("failed to commit transaction: %v", err)
	}

	var count int
	db.QueryOne(ctx, "SELECT COUNT(*) FROM users").Scan(&count)

	if count != 1 {
		t.Errorf("expected 1 row after commit, got %d", count)
	}
}

func TestBeginTxRollback(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	_, _ = db.Exec(ctx, `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("failed to begin transaction: %v", err)
	}

	_, err = tx.Exec("INSERT INTO users (name) VALUES (?)", "Alice")
	if err != nil {
		t.Fatalf("failed to insert in transaction: %v", err)
	}

	err = tx.Rollback()
	if err != nil {
		t.Fatalf("failed to rollback transaction: %v", err)
	}

	var count int
	db.QueryOne(ctx, "SELECT COUNT(*) FROM users").Scan(&count)

	if count != 0 {
		t.Errorf("expected 0 rows after rollback, got %d", count)
	}
}

func TestBeginTxClosed(t *testing.T) {
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
}

func TestConfigDefaults(t *testing.T) {
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
}

func TestConfigDevelopment(t *testing.T) {
	cfg := DevelopmentConfig()

	if cfg.MaxOpenConns != 10 {
		t.Errorf("expected MaxOpenConns 10, got %d", cfg.MaxOpenConns)
	}

	if cfg.Pragmas["journal_mode"] != "DELETE" {
		t.Errorf("expected journal_mode DELETE, got %s", cfg.Pragmas["journal_mode"])
	}
}

func TestConfigProduction(t *testing.T) {
	cfg := ProductionConfig()

	if cfg.MaxOpenConns != 100 {
		t.Errorf("expected MaxOpenConns 100, got %d", cfg.MaxOpenConns)
	}

	if cfg.Pragmas["foreign_keys"] != "ON" {
		t.Errorf("expected foreign_keys ON, got %s", cfg.Pragmas["foreign_keys"])
	}
}

func TestConfigChaining(t *testing.T) {
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
}

func TestConnectionPool(t *testing.T) {
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
