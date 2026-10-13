package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type sumAggregate struct{ total int64 }

func (s *sumAggregate) Step(_ *FunctionContext, args []driver.Value) error {
	n, ok := args[0].(int64)
	if !ok {
		return errors.New("test_sum wants an integer")
	}
	s.total += n
	return nil
}

func (s *sumAggregate) WindowInverse(_ *FunctionContext, args []driver.Value) error {
	n, ok := args[0].(int64)
	if !ok {
		return errors.New("test_sum wants an integer")
	}
	s.total -= n
	return nil
}

func (s *sumAggregate) WindowValue(*FunctionContext) (driver.Value, error) { return s.total, nil }

func (s *sumAggregate) Final(*FunctionContext) {}

func slug(_ *FunctionContext, args []driver.Value) (driver.Value, error) {
	title, ok := args[0].(string)
	if !ok {
		return nil, errors.New("slug wants text")
	}
	return strings.ReplaceAll(strings.ToLower(title), " ", "-"), nil
}

// Registration is process-global and a name registers once, so -count=2 must
// not register again.
var registerTestFunctions = sync.OnceValue(func() error {
	return errors.Join(
		RegisterDeterministicScalarFunction("test_slug", 1, slug),
		RegisterScalarFunction("test_plain_slug", 1, slug),
		RegisterFunction("test_sum", &FunctionImpl{
			NArgs:         1,
			MakeAggregate: func(FunctionContext) (AggregateFunction, error) { return &sumAggregate{}, nil },
		}),
	)
})

func TestRegisterFunction(t *testing.T) {
	ctx := context.Background()
	if err := registerTestFunctions(); err != nil {
		t.Fatal(err)
	}
	if err := RegisterScalarFunction("test_slug", 1, slug); err == nil {
		t.Error("registering a name twice succeeded")
	}
	path := filepath.Join(t.TempDir(), "functions.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// SQLite accepts only a deterministic function in a generated column and
	// in an index expression.
	if _, schemaErr := db.Exec(ctx, `
		CREATE TABLE docs (id INTEGER PRIMARY KEY, title TEXT NOT NULL, slug TEXT GENERATED ALWAYS AS (test_slug(title)) STORED);
		CREATE UNIQUE INDEX docs_slug ON docs (test_slug(title));`); schemaErr != nil {
		t.Fatal(schemaErr)
	}
	_, err = db.Exec(ctx, "CREATE INDEX docs_plain ON docs (test_plain_slug(title))")
	if err == nil || !strings.Contains(err.Error(), "non-deterministic") {
		t.Fatalf("plain function in an index: %v", err)
	}

	var inWrite string
	err = db.Write(ctx, func(tx *sql.Tx) error {
		if _, insertErr := tx.Exec("INSERT INTO docs (title) VALUES ('Hello World'), ('Second Doc')"); insertErr != nil {
			return insertErr
		}
		return tx.QueryRow("SELECT test_slug(title) FROM docs WHERE id = 1").Scan(&inWrite)
	})
	if err != nil || inWrite != "hello-world" {
		t.Fatalf("Write: slug=%q err=%v", inWrite, err)
	}
	err = db.Write(ctx, func(tx *sql.Tx) error {
		_, insertErr := tx.Exec("INSERT INTO docs (title) VALUES ('hello world')")
		return insertErr
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("the expression index must reject a duplicate slug: %v", err)
	}

	ro, err := OpenReadOnly(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	for name, handle := range map[string]*Database{"Open": db, "OpenReadOnly": ro} {
		rows, err := handle.Query(ctx, "SELECT id, slug, test_plain_slug(title) FROM docs WHERE test_slug(title) = ?", "second-doc")
		if err != nil {
			t.Fatal(err)
		}
		var id int
		var stored, plain string
		if !rows.Next() {
			t.Fatalf("%s: no row for the slug: %v", name, rows.Err())
		}
		if err := errors.Join(rows.Scan(&id, &stored, &plain), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if id != 2 || stored != "second-doc" || plain != "second-doc" {
			t.Errorf("%s: id=%d stored=%q plain=%q", name, id, stored, plain)
		}
		var plan string
		if err := handle.QueryOne(ctx, "EXPLAIN QUERY PLAN SELECT id FROM docs WHERE test_slug(title) = 'x'").Scan(new(int), new(int), new(int), &plan); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan, "docs_slug") {
			t.Errorf("%s: query does not use the expression index: %s", name, plan)
		}
		var sum int
		if err := handle.QueryOne(ctx, "SELECT test_sum(id) FROM docs").Scan(&sum); err != nil || sum != 3 {
			t.Errorf("%s: test_sum=%d err=%v, want 3", name, sum, err)
		}
	}
}
