package migrations

import (
	"context"
	"database/sql"
	"testing"
)

func TestRegister(t *testing.T) {
	// Reset registry before test
	Reset()
	defer Reset()

	tests := []struct {
		migration   Migration
		name        string
		panicMsg    string
		expectPanic bool
	}{
		{
			name: "valid migration",
			migration: Migration{
				Version:     "20251107000001",
				Description: "test_migration",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
				Down: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: false,
		},
		{
			name: "duplicate version",
			migration: Migration{
				Version:     "20251107000001",
				Description: "duplicate",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: true,
			panicMsg:    "already registered",
		},
		{
			name: "empty version",
			migration: Migration{
				Version:     "",
				Description: "empty_version",
				Up: func(ctx context.Context, db *sql.DB) error {
					return nil
				},
			},
			expectPanic: true,
			panicMsg:    "cannot be empty",
		},
		{
			name: "missing up function",
			migration: Migration{
				Version:     "20251107000002",
				Description: "no_up",
				Up:          nil,
			},
			expectPanic: true,
			panicMsg:    "missing Up function",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("expected panic but didn't get one")
					}
				}()
			}

			Register(tt.migration)

			if !tt.expectPanic {
				// Verify migration was registered
				registryMu.RLock()
				_, exists := registry[tt.migration.Version]
				registryMu.RUnlock()

				if !exists {
					t.Errorf("migration not found in registry")
				}
			}
		})
	}
}
