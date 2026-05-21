package migrations

import (
	"context"
	"database/sql"
	"testing"

	"github.com/dnl-fm/go-sqlite/pkg/database"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	ctx := context.Background()
	db, err := database.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}

	return db.DB()
}
