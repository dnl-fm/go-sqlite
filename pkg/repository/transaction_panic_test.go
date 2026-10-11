package repository

import (
	"context"
	"github.com/dnl-fm/go-sqlite/pkg/database"
	"github.com/dnl-fm/go-sqlite/pkg/query"
	"testing"
	"time"
)

func TestWithTxPanicReleasesWriter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, openErr := database.Open(ctx, t.TempDir()+"/panic.db")
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer db.Close()
	if _, err := db.Exec(ctx, "CREATE TABLE users(id TEXT PRIMARY KEY, name TEXT, email TEXT, age INTEGER)"); err != nil {
		t.Fatal(err)
	}
	repo := New[testUser, string](db.DB(), "users")
	func() {
		defer func() {
			if p := recover(); p != "boom" {
				t.Fatalf("panic=%v", p)
			}
		}()
		_ = repo.WithTx(ctx, func(tx *Repository[testUser, string]) error {
			q, err := query.Build("INSERT INTO users(id,name) VALUES(:id,:name)", map[string]any{"id": "rolled-back", "name": "panic"})
			if err != nil {
				t.Fatal(err)
			}
			if _, insertErr := tx.Insert(ctx, q); insertErr != nil {
				t.Fatal(insertErr)
			}
			panic("boom")
		})
	}()
	if _, err := db.Exec(ctx, "INSERT INTO users(id,name) VALUES('committed','after panic')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryOne(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows=%d want 1", count)
	}
}
