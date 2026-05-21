package database

import (
	"context"
	"strings"
	"testing"
)

func TestExecRejectsWithoutRowID(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(ctx, `CREATE TABLE lookup (id TEXT PRIMARY KEY) WITHOUT ROWID`)
	if err == nil {
		t.Fatal("expected WITHOUT ROWID create to fail")
	}
	if !strings.Contains(err.Error(), "requires rowid tables") {
		t.Fatalf("expected rowid requirement error, got %v", err)
	}
}

func TestValidateRowIDSchemaAllowsNormalTables(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec(ctx, `CREATE TABLE lookup (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("failed to create rowid table: %v", err)
	}
	if err := ValidateRowIDSchema(ctx, db.DB()); err != nil {
		t.Fatalf("expected rowid schema to pass: %v", err)
	}
}
