package database

import (
	"context"
	"errors"
	"testing"
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
	t.Run("keeps Turso MVCC driver", func(t *testing.T) {
		ctx := context.Background()
		db, err := Open(ctx, ":memory:", WithDriver("sqlite"))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db.Config().Driver != DefaultDriver {
			t.Errorf("expected driver %q, got %s", DefaultDriver, db.Config().Driver)
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

	t.Run("normalizes config driver", func(t *testing.T) {
		ctx := context.Background()
		cfg := DefaultConfig().WithDriver("other")
		db, err := Open(ctx, ":memory:", WithConfig(cfg), WithDriver("sqlite"))
		if err != nil {
			t.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()

		if db.Config().Driver != DefaultDriver {
			t.Errorf("expected driver %q after override, got %s", DefaultDriver, db.Config().Driver)
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
