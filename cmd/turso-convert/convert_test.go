package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Copy while the writer is open, then close it: only the snapshot retains
// pending committed transactions. The converter never opens the live fixture.
func fixture(t *testing.T, kind string) (string, fixtureEvidence, int64) {
	t.Helper()
	live := filepath.Join(t.TempDir(), "live.db")
	driver := "turso"
	if strings.HasPrefix(kind, "plain") {
		driver = "sqlite"
	}
	db, err := sql.Open(driver, live)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if kind == "Turso MVCC" {
		execFixture(t, db, "PRAGMA journal_mode=mvcc")
	}
	if kind == "plain WAL" {
		execFixture(t, db, "PRAGMA journal_mode=WAL")
	}
	for _, q := range []string{
		`CREATE TABLE parents(id INTEGER PRIMARY KEY, label TEXT UNIQUE NOT NULL)`,
		`CREATE TABLE items(id INTEGER PRIMARY KEY AUTOINCREMENT, parent INTEGER REFERENCES parents(id), text TEXT, data BLOB, n REAL)`,
		`CREATE INDEX items_parent ON items(parent)`,
		`CREATE TABLE "odd "" table"(v TEXT)`,
		`CREATE VIEW item_names AS SELECT id,text FROM items`,
		`INSERT INTO parents VALUES(1, 'one'), (2, 'two')`,
		`INSERT INTO "odd "" table" VALUES ('quotes')`,
	} {
		execFixture(t, db, q)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO items(parent,text,data,n) VALUES(?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 10001 {
		if _, err = stmt.Exec(i%2+1, fmt.Sprintf("row-%d", i), []byte{0, 1, byte(i), 255}, float64(i)/3); err != nil {
			t.Fatal(err)
		}
	}
	if err = stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if driver == "turso" || kind == "plain WAL" {
		execFixture(t, db, "PRAGMA wal_checkpoint(TRUNCATE)")
	}
	execFixture(t, db, `INSERT INTO items(id,parent,text,data,n) VALUES(20000,1,'deleted high watermark',x'1234',1)`)
	execFixture(t, db, `DELETE FROM items WHERE id=20000`)
	execFixture(t, db, `INSERT INTO parents VALUES(3, 'pending')`)
	evidence := fixtureEvidence{Content: contentChecksums(t, db), Schema: schemaEvidence(t, db), Counts: countEvidence(t, db)}
	var seq int64
	if err = db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='items'`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "source.db")
	for _, suffix := range sourceSuffixes {
		data, readErr := os.ReadFile(live + suffix)
		if os.IsNotExist(readErr) {
			continue
		}
		if readErr != nil {
			t.Fatal(readErr)
		}
		if err = os.WriteFile(src+suffix, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if kind == "Turso MVCC" {
		pending, err := tursoLogPending(src + "-log")
		if err != nil || !pending {
			t.Fatalf("fixture needs pending log: %v %v", pending, err)
		}
	}
	return src, evidence, seq
}

type fixtureEvidence struct {
	Content map[string][32]byte
	Schema  []string
	Counts  map[string]int64
}

// These queries are independent of the converter's record and comparison code.
func schemaEvidence(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT type,name,tbl_name,sql FROM sqlite_schema WHERE name NOT GLOB '__turso_internal_*' AND name != 'sqlite_sequence' ORDER BY type,name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var kind, name, table string
		var statement sql.NullString
		if err = rows.Scan(&kind, &name, &table, &statement); err != nil {
			t.Fatal(err)
		}
		result = append(result, fmt.Sprintf("%q %q %q %t %q", kind, name, table, statement.Valid, statement.String))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func countEvidence(t *testing.T, db *sql.DB) map[string]int64 {
	t.Helper()
	result := map[string]int64{}
	for _, name := range []string{"parents", "items", `odd " table`} {
		var n int64
		if err := db.QueryRow("SELECT count(*) FROM " + quote(name)).Scan(&n); err != nil {
			t.Fatal(err)
		}
		result[name] = n
	}
	return result
}

func execFixture(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(q, err)
	}
}

func fileHashes(t *testing.T, p string) map[string][32]byte {
	t.Helper()
	got := map[string][32]byte{}
	for _, s := range sourceSuffixes {
		b, err := os.ReadFile(p + s)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		got[s] = sha256.Sum256(b)
	}
	return got
}

// Sort encoded rows to compare values without relying on engine row order.
func contentChecksums(t *testing.T, db *sql.DB) map[string][32]byte {
	t.Helper()
	got := map[string][32]byte{}
	for _, name := range []string{"parents", "items", `odd " table`} {
		rows, err := db.Query("SELECT * FROM " + quote(name))
		if err != nil {
			t.Fatal(err)
		}
		cols, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var enc []string
		for rows.Next() {
			v := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range v {
				dest[i] = &v[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			for _, x := range v {
				fmt.Fprintf(&b, "%T:%v;", x, x)
			}
			enc = append(enc, b.String())
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(enc)
		got[name] = sha256.Sum256([]byte(strings.Join(enc, "\n")))
	}
	return got
}

func TestConvertFormats(t *testing.T) {
	for _, kind := range []string{"Turso MVCC", "Turso WAL", "plain rollback", "plain WAL"} {
		t.Run(kind, func(t *testing.T) {
			src, want, seq := fixture(t, kind)
			before := fileHashes(t, src)
			classification, err := check(context.Background(), src)
			if err != nil {
				t.Fatal(err)
			}
			wantKind := kind
			if strings.HasPrefix(kind, "plain") {
				wantKind = "plain SQLite"
			}
			if classification != wantKind {
				t.Fatalf("classification %q want %q", classification, wantKind)
			}
			dst := filepath.Join(t.TempDir(), "converted.db")
			report, err := convert(context.Background(), src, dst)
			if err != nil {
				t.Fatal(err)
			}
			if report.Tables != 3 || report.Rows != 10005 {
				t.Fatalf("report: %+v", report)
			}
			if !reflect.DeepEqual(before, fileHashes(t, src)) {
				t.Fatal("source changed")
			}
			db, err := sql.Open("sqlite", dst)
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			defer db.Close()
			if got := contentChecksums(t, db); !reflect.DeepEqual(got, want.Content) {
				t.Fatalf("content mismatch: got %v want %v", got, want.Content)
			}
			if got := schemaEvidence(t, db); !reflect.DeepEqual(got, want.Schema) {
				t.Fatalf("schema mismatch: got %v want %v", got, want.Schema)
			}
			if got := countEvidence(t, db); !reflect.DeepEqual(got, want.Counts) {
				t.Fatalf("counts mismatch: got %v want %v", got, want.Counts)
			}
			var integrity, mode string
			if err = db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
				t.Fatalf("integrity %s %v", integrity, err)
			}
			if err = db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
				t.Fatalf("mode %s %v", mode, err)
			}
			var n int
			if err = db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name GLOB '__turso_internal_*'`).Scan(&n); err != nil || n != 0 {
				t.Fatalf("internal objects %d %v", n, err)
			}
			execFixture(t, db, `INSERT INTO items(parent,text) VALUES(1,'after conversion')`)
			var id int64
			if err = db.QueryRow("SELECT max(id) FROM items").Scan(&id); err != nil || id != seq+1 {
				t.Fatalf("next id %d want %d: %v", id, seq+1, err)
			}
		})
	}
}
