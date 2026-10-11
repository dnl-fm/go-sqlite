package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	_ "modernc.org/sqlite"
	_ "turso.tech/database/tursogo"
)

const sequencePrefix = "__turso_internal_seq___turso_internal_autoincrement_"

type conversionReport struct {
	Kind                          string
	Tables                        int
	Rows                          int64
	SourceBytes, DestinationBytes int64
	Duration                      time.Duration
}

type schemaObject struct {
	Type, Name, Table string
	SQL               sql.NullString
}
type databaseRecord struct {
	Schema    []schemaObject
	Counts    map[string]int64
	Internal  []string
	Sequences map[string]int64
}

func nativeError(err *error) {
	if p := recover(); p != nil {
		*err = fmt.Errorf("Turso driver panic: %v; verify that the pinned native library is available and the source is supported", p)
	}
}

func convert(ctx context.Context, src, dst string) (report conversionReport, err error) {
	defer nativeError(&err)
	start := time.Now()
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if err = destinationFree(dst); err != nil {
		return report, err
	}
	before, err := snapshot(src)
	if err != nil {
		return report, err
	}
	dir, err := os.MkdirTemp(filepath.Dir(dst), ".turso-convert-")
	if err != nil {
		return report, err
	}
	defer os.RemoveAll(dir)
	staged, err := stage(src, dir, before)
	if err != nil {
		return report, err
	}
	// Turso DSNs split at '?'; the staged name is fixed but its directory
	// can contain this character. Refuse before an engine can open another path.
	if strings.Contains(staged, "?") {
		return report, fmt.Errorf("destination directory contains '?', unsupported by the Turso driver")
	}
	report.Kind, err = classify(ctx, staged)
	if err != nil {
		return report, err
	}
	var original databaseRecord
	if report.Kind == "plain SQLite" {
		original, err = recordSQLite(ctx, staged)
	} else {
		original, err = checkpointTurso(ctx, staged, report.Kind)
	}
	if err != nil {
		return report, fmt.Errorf("read staged source: %w", err)
	}
	if report.Kind != "plain SQLite" {
		if err = patchHeader(staged); err != nil {
			return report, err
		}
		for _, suffix := range []string{"-log", "-wal", "-shm"} {
			if err = os.Rename(staged+suffix, staged+suffix+".turso"); err != nil && !errors.Is(err, os.ErrNotExist) {
				return report, err
			}
		}
	}
	if err = finishSQLite(ctx, staged, original); err != nil {
		return report, err
	}
	if err = unchanged(src, before); err != nil {
		return report, err
	}
	if err = ctx.Err(); err != nil {
		return report, err
	}
	if err = destinationFree(dst); err != nil {
		return report, err
	}
	f, err := os.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return report, err
	}
	info, statErr := f.Stat()
	err = errors.Join(statErr, f.Chmod(before[""].info.Mode().Perm()), f.Sync(), f.Close())
	if err != nil {
		return report, err
	}
	if err = install(staged, dst); err != nil {
		return report, err
	}
	report.Tables = len(original.Counts)
	for _, count := range original.Counts {
		report.Rows += count
	}
	for _, file := range before {
		report.SourceBytes += file.info.Size()
	}
	report.DestinationBytes = info.Size()
	report.Duration = time.Since(start).Round(time.Millisecond)
	return report, nil
}

func check(ctx context.Context, src string) (kind string, err error) {
	defer nativeError(&err)
	if err = ctx.Err(); err != nil {
		return "", err
	}
	before, err := snapshot(src)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "turso-check-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	staged, err := stage(src, dir, before)
	if err != nil {
		return "", err
	}
	kind, err = classify(ctx, staged)
	if err != nil {
		return "", err
	}
	if kind != "plain SQLite" {
		_, err = checkpointTurso(ctx, staged, kind)
	} else {
		_, err = recordSQLite(ctx, staged)
	}
	if err != nil {
		return "", err
	}
	return kind, unchanged(src, before)
}

func header(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b := make([]byte, 100)
	if _, err = io.ReadFull(f, b); err != nil {
		return nil, fmt.Errorf("invalid or truncated database header: %w", err)
	}
	if string(b[:16]) != "SQLite format 3\x00" {
		return nil, fmt.Errorf("invalid database magic")
	}
	page := int64(binary.BigEndian.Uint16(b[16:18]))
	if page == 1 {
		page = 65536
	}
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if page < 512 || page > 65536 || page&(page-1) != 0 || info.Size() < page || info.Size()%page != 0 {
		return nil, fmt.Errorf("invalid page size or truncated database: page=%d bytes=%d", page, info.Size())
	}
	return b, nil
}

func classify(ctx context.Context, path string) (string, error) {
	b, err := header(path)
	if err != nil {
		return "", err
	}
	if b[18] == 255 && b[19] == 255 {
		return "Turso MVCC", nil
	}
	if (b[18] != 1 || b[19] != 1) && (b[18] != 2 || b[19] != 2) {
		return "", fmt.Errorf("unsupported database header versions %x", b[18:20])
	}
	if pending, err := tursoLogPending(path + "-log"); err != nil {
		return "", err
	} else if pending {
		return "", fmt.Errorf("logical log exists without an MVCC header; recover with the owning Turso version first")
	}
	db, err := openDB("sqlite", path, true)
	if err != nil {
		return "", err
	}
	defer db.Close()
	var n int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name GLOB '__turso_internal_*'`).Scan(&n); err != nil {
		return "", fmt.Errorf("read SQLite schema: %w", err)
	}
	if n > 0 {
		return "Turso WAL", nil
	}
	return "plain SQLite", nil
}

func openDB(driver, path string, readonly bool) (*sql.DB, error) {
	var dsn string
	if driver == "sqlite" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: absolute}
		q := url.Values{"_pragma": {"busy_timeout(1000)"}}
		if readonly {
			q.Set("mode", "ro")
		}
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn = path + "?_busy_timeout=1000"
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func quote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

func record(ctx context.Context, db *sql.DB) (databaseRecord, error) {
	r := databaseRecord{Counts: map[string]int64{}, Sequences: map[string]int64{}}
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name`)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var obj schemaObject
		if err = rows.Scan(&obj.Type, &obj.Name, &obj.Table, &obj.SQL); err != nil {
			rows.Close()
			return r, err
		}
		if strings.HasPrefix(obj.Name, "__turso_internal_") {
			if obj.Type != "table" || (obj.Name != "__turso_internal_mvcc_meta" && !strings.HasPrefix(obj.Name, sequencePrefix)) {
				rows.Close()
				return r, fmt.Errorf("unsupported Turso internal object %q; use its owning engine to export this database", obj.Name)
			}
			r.Internal = append(r.Internal, obj.Name)
			continue
		}
		if obj.Name != "sqlite_sequence" {
			r.Schema = append(r.Schema, obj)
		}
	}
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return r, err
	}
	for _, obj := range r.Schema {
		if obj.Type == "table" {
			var n int64
			if err = db.QueryRowContext(ctx, "SELECT count(*) FROM "+quote(obj.Name)).Scan(&n); err != nil {
				return r, fmt.Errorf("count %q: %w", obj.Name, err)
			}
			r.Counts[obj.Name] = n
		}
	}
	var hasSequence int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_schema WHERE name='sqlite_sequence'`).Scan(&hasSequence); err != nil {
		return r, err
	}
	if hasSequence > 0 {
		rows, err = db.QueryContext(ctx, `SELECT name,seq FROM sqlite_sequence ORDER BY name`)
		if err != nil {
			return r, err
		}
		for rows.Next() {
			var name string
			var seq int64
			if err = rows.Scan(&name, &seq); err != nil {
				rows.Close()
				return r, err
			}
			if old, ok := r.Sequences[name]; !ok || seq > old {
				r.Sequences[name] = seq
			}
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return r, err
		}
	}
	for _, name := range r.Internal {
		if !strings.HasPrefix(name, sequencePrefix) {
			continue
		}
		table := strings.TrimPrefix(name, sequencePrefix)
		if _, ok := r.Counts[table]; !ok {
			return r, fmt.Errorf("Turso sequence %q has no user table", name)
		}
		var value, called int64
		if err = db.QueryRowContext(ctx, "SELECT value,is_called FROM "+quote(name)).Scan(&value, &called); err != nil {
			return r, fmt.Errorf("read Turso sequence %q: %w", name, err)
		}
		if seq, ok := r.Sequences[table]; called != 0 && (!ok || value > seq) {
			r.Sequences[table] = value
		}
	}
	return r, nil
}

func integrity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var s string
		if err = rows.Scan(&s); err != nil {
			return err
		}
		results = append(results, s)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("SQLite integrity_check: %s", strings.Join(results, "; "))
	}
	return nil
}

func recordSQLite(ctx context.Context, path string) (r databaseRecord, err error) {
	db, err := openDB("sqlite", path, true)
	if err != nil {
		return r, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err = integrity(ctx, db); err != nil {
		return r, err
	}
	return record(ctx, db)
}

func checkpointTurso(ctx context.Context, path, kind string) (r databaseRecord, err error) {
	defer nativeError(&err)
	if _, err = tursoLogPending(path + "-log"); err != nil {
		return r, err
	}
	db, err := openDB("turso", path, false)
	if err != nil {
		return r, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if kind == "Turso MVCC" {
		var mode string
		if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=mvcc").Scan(&mode); err != nil {
			return r, err
		}
		if mode != "mvcc" {
			return r, fmt.Errorf("Turso refused MVCC recovery: %s", mode)
		}
	}
	if _, err = db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return r, err
	}
	r, err = record(ctx, db)
	if err != nil {
		return r, err
	}
	if err = db.Close(); err != nil {
		return r, err
	}
	if pending, e := tursoLogPending(path + "-log"); e != nil {
		return r, e
	} else if pending {
		return r, fmt.Errorf("Turso checkpoint left pending logical-log transactions")
	}
	if info, e := os.Stat(path + "-wal"); e == nil && info.Size() != 0 {
		return r, fmt.Errorf("Turso checkpoint left a nonempty WAL")
	} else if e != nil && !errors.Is(e, os.ErrNotExist) {
		return r, e
	}
	return r, nil
}

func patchHeader(path string) error {
	b, err := header(path)
	if err != nil {
		return err
	}
	if b[18] != 255 || b[19] != 255 {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	_, writeErr := f.WriteAt([]byte{2, 2}, 18)
	return errors.Join(writeErr, f.Sync(), f.Close())
}

func finishSQLite(ctx context.Context, path string, original databaseRecord) (err error) {
	db, err := openDB("sqlite", path, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if _, err = db.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	for _, name := range original.Internal {
		if _, err = db.ExecContext(ctx, "DROP TABLE "+quote(name)); err != nil {
			return fmt.Errorf("remove Turso internal table %q: %w", name, err)
		}
	}
	for table, seq := range original.Sequences {
		if _, err = db.ExecContext(ctx, "DELETE FROM sqlite_sequence WHERE name=?", table); err != nil {
			return err
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO sqlite_sequence(name,seq) VALUES(?,?)", table, seq); err != nil {
			return err
		}
	}
	var mode string
	if err = db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		return err
	}
	if mode != "wal" {
		return fmt.Errorf("SQLite refused WAL mode: %s", mode)
	}
	if err = integrity(ctx, db); err != nil {
		return fmt.Errorf("verify output: %w", err)
	}
	got, err := record(ctx, db)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(original.Schema, got.Schema) {
		return fmt.Errorf("SQLite schema differs from source")
	}
	if !reflect.DeepEqual(original.Counts, got.Counts) {
		return fmt.Errorf("SQLite table row counts differ from source: source=%v SQLite=%v", original.Counts, got.Counts)
	}
	if !reflect.DeepEqual(original.Sequences, got.Sequences) {
		return fmt.Errorf("SQLite AUTOINCREMENT sequences differ from source: source=%v SQLite=%v", original.Sequences, got.Sequences)
	}
	var busy, log, done int
	if err = db.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &done); err != nil {
		return err
	}
	if busy != 0 || log != 0 || done != 0 {
		return fmt.Errorf("SQLite checkpoint incomplete: busy=%d log=%d checkpointed=%d", busy, log, done)
	}
	return db.Close()
}
