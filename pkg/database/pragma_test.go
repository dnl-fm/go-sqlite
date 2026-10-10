package database

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOpenConfiguresEveryPoolConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := Open(ctx, t.TempDir()+"/pool.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Hold each connection so the next reservation must use another native connection.
	for i := 0; i < db.Config().MaxOpenConns; i++ {
		conn, err := db.DB().Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		assertConnectionPragmas(t, ctx, conn, i)
	}
}

func TestOpenConfiguresConnectionAfterPoolGrows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := DefaultConfig().WithMaxOpenConns(1).WithMaxIdleConns(1)
	db, err := Open(ctx, t.TempDir()+"/grow.db", WithConfig(cfg))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	first, err := db.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	assertConnectionPragmas(t, ctx, first, 0)

	db.DB().SetMaxOpenConns(2)
	second, err := db.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	assertConnectionPragmas(t, ctx, second, 1)
}

func assertConnectionPragmas(t *testing.T, ctx context.Context, conn *sql.Conn, index int) {
	t.Helper()
	for _, pragma := range []struct {
		name string
		want int
	}{
		{"foreign_keys", 1},
		{"busy_timeout", 1000},
		{"synchronous", 1},
		{"cache_size", -64000},
	} {
		var got int
		if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma.name).Scan(&got); err != nil {
			t.Errorf("connection %d %s: %v", index, pragma.name, err)
		} else if got != pragma.want {
			t.Errorf("connection %d %s = %d, want %d", index, pragma.name, got, pragma.want)
		}
	}
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Errorf("connection %d journal_mode: %v", index, err)
	} else if mode != "mvcc" {
		t.Errorf("connection %d journal_mode = %q, want mvcc", index, mode)
	}
}

func TestOpenStrictPragmaFailure(t *testing.T) {
	cfg := DefaultConfig().WithPragma("foreign_keys", ")").WithStrictPragmas(true)
	db, err := Open(context.Background(), t.TempDir()+"/strict.db", WithConfig(cfg))
	if db != nil {
		db.Close()
		t.Fatal("expected no database after failed pragma")
	}
	if err == nil || !strings.Contains(err.Error(), "PRAGMA foreign_keys = )") {
		t.Fatalf("expected strict pragma error from Open, got %v", err)
	}
}

func TestPragmaConnector(t *testing.T) {
	pragmaErr := errors.New("pragma rejected")
	for _, test := range []struct {
		execErr    error
		nextErr    error
		closeErr   error
		name       string
		mode       string
		strict     bool
		wantErr    bool
		wantClosed bool
	}{
		{name: "ordered then sorted", mode: "mvcc"},
		{name: "non-strict continues", execErr: pragmaErr, mode: "mvcc"},
		{name: "strict closes connection", strict: true, execErr: pragmaErr, mode: "mvcc", wantErr: true, wantClosed: true},
		{name: "journal mismatch always fails", mode: "wal", wantErr: true, wantClosed: true},
		{name: "journal step error always fails", nextErr: pragmaErr, wantErr: true, wantClosed: true},
		{name: "journal close error always fails", mode: "mvcc", closeErr: pragmaErr, wantErr: true, wantClosed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			native := &pragmaTestConn{execErr: test.execErr, rows: &pragmaTestRows{mode: test.mode, nextErr: test.nextErr, closeErr: test.closeErr}}
			connector := &pragmaConnector{
				driver: &pragmaTestDriver{conn: native},
				config: &Config{
					Pragmas:       map[string]string{"journal_mode": "'mvcc'", "b": "2", "a": "1", "z": "3"},
					PragmaOrder:   []string{"missing", "journal_mode", "z"},
					StrictPragmas: test.strict,
				},
			}
			conn, err := connector.Connect(context.Background())
			if (err != nil) != test.wantErr {
				t.Fatalf("Connect error = %v, want error %t", err, test.wantErr)
			}
			if native.closed != test.wantClosed {
				t.Fatalf("connection closed = %t, want %t", native.closed, test.wantClosed)
			}
			if !native.rows.closed {
				t.Fatal("journal rows were not closed")
			}
			if test.execErr != nil && test.strict || test.nextErr != nil {
				if !errors.Is(err, pragmaErr) {
					t.Fatalf("original pragma error lost: %v", err)
				}
			}
			if !test.wantErr {
				defer conn.Close()
				want := []string{"PRAGMA journal_mode = 'mvcc'", "PRAGMA z = 3", "PRAGMA a = 1", "PRAGMA b = 2"}
				if !reflect.DeepEqual(native.queries, want) {
					t.Fatalf("pragma order = %v, want %v", native.queries, want)
				}
			}
		})
	}
}

type pragmaTestDriver struct{ conn *pragmaTestConn }

func (d *pragmaTestDriver) Open(string) (driver.Conn, error) { return d.conn, nil }

type pragmaTestConn struct {
	rows    *pragmaTestRows
	execErr error
	queries []string
	closed  bool
}

func (c *pragmaTestConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (c *pragmaTestConn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c *pragmaTestConn) Close() error                        { c.closed = true; return nil }
func (c *pragmaTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	c.queries = append(c.queries, query)
	return driver.RowsAffected(0), c.execErr
}
func (c *pragmaTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	c.queries = append(c.queries, query)
	return c.rows, nil
}

type pragmaTestRows struct {
	nextErr  error
	closeErr error
	mode     string
	closed   bool
	read     bool
}

func (r *pragmaTestRows) Columns() []string { return []string{"journal_mode"} }
func (r *pragmaTestRows) Close() error      { r.closed = true; return r.closeErr }
func (r *pragmaTestRows) Next(dest []driver.Value) error {
	if r.nextErr != nil {
		return r.nextErr
	}
	if r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = r.mode
	return nil
}
