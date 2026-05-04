package database

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
		WithConnMaxLifetime(8*time.Minute).
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
