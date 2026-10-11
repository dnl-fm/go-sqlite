package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDestinationRefused(t *testing.T) {
	src, _, _ := fixture(t, "plain rollback")
	before := fileHashes(t, src)
	for _, suffix := range sourceSuffixes {
		t.Run(suffix, func(t *testing.T) {
			dst := filepath.Join(t.TempDir(), "target.db")
			want := []byte("keep")
			if err := os.WriteFile(dst+suffix, want, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := convert(context.Background(), src, dst); err == nil {
				t.Fatal("accepted existing destination")
			}
			b, err := os.ReadFile(dst + suffix)
			if err != nil || !bytes.Equal(b, want) {
				t.Fatal("destination changed")
			}
			if suffix != "" {
				if _, err := os.Stat(dst); !os.IsNotExist(err) {
					t.Fatal("created output")
				}
			}
		})
	}
	if !reflect.DeepEqual(before, fileHashes(t, src)) {
		t.Fatal("source changed")
	}
}

func TestInvalidSource(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("garbage"), append([]byte("SQLite format 3\x00"), make([]byte, 84)...)} {
		src := filepath.Join(t.TempDir(), "bad.db")
		dst := src + ".out"
		if err := os.WriteFile(src, data, 0600); err != nil {
			t.Fatal(err)
		}
		before := fileHashes(t, src)
		if _, err := convert(context.Background(), src, dst); err == nil {
			t.Fatal("accepted corrupt file")
		}
		if _, err := check(context.Background(), src); err == nil {
			t.Fatal("classified corrupt file")
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Fatal("created output")
		}
		if !reflect.DeepEqual(before, fileHashes(t, src)) {
			t.Fatal("source changed")
		}
		assertNoStaging(t, dst)
	}
}

func TestCorruptLogRefused(t *testing.T) {
	for _, kind := range []string{"short", "truncated", "checksum"} {
		t.Run(kind, func(t *testing.T) {
			src, _, _ := fixture(t, "Turso MVCC")
			b, err := os.ReadFile(src + "-log")
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "short":
				b = []byte("broken")
			case "truncated":
				b = b[:len(b)-1]
			case "checksum":
				b[len(b)-8] ^= 1
			}
			if err = os.WriteFile(src+"-log", b, 0600); err != nil {
				t.Fatal(err)
			}
			before := fileHashes(t, src)
			dst := src + ".out"
			if _, err = convert(context.Background(), src, dst); err == nil {
				t.Fatal("accepted corrupt log")
			}
			if _, err = os.Stat(dst); !os.IsNotExist(err) {
				t.Fatal("created output")
			}
			if !reflect.DeepEqual(before, fileHashes(t, src)) {
				t.Fatal("source changed")
			}
			assertNoStaging(t, dst)
		})
	}
}

func TestSourceMutationDetected(t *testing.T) {
	src := filepath.Join(t.TempDir(), "source.db")
	if err := os.WriteFile(src, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := snapshot(src)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(src, []byte("xyz"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(src, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = unchanged(src, before); err == nil {
		t.Fatal("same-size same-mtime content mutation undetected")
	}
	if err = os.WriteFile(src, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chtimes(src, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err = unchanged(src, before); err != nil {
		t.Fatal("restored base differs:", err)
	}
	if err = os.WriteFile(src+"-log", []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = unchanged(src, before); err == nil {
		t.Fatal("new sidecar undetected")
	}
	for _, suffix := range []string{"-log", "-wal"} {
		if err = os.WriteFile(src+suffix, []byte("old"), 0600); err != nil {
			t.Fatal(err)
		}
		before, err = snapshot(src)
		if err != nil {
			t.Fatal(err)
		}
		info, err = os.Stat(src + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(src+suffix, []byte("new"), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Chtimes(src+suffix, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if err = unchanged(src, before); err == nil {
			t.Fatalf("same-size same-mtime %s mutation undetected", suffix)
		}
	}
}

func TestAtomicInstallNoReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "staged")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := install(src, dst); err == nil {
		t.Fatal("overwrote destination")
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "old" {
		t.Fatal("destination overwritten")
	}
}

func TestLockedSource(t *testing.T) {
	if p := os.Getenv("CONVERTER_LOCK_CHILD"); p != "" {
		f, err := os.OpenFile(p, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if os.Getenv("CONVERTER_LOCK_SCHEME") == "flock" {
			err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		} else {
			lock := unix.Flock_t{Type: unix.F_WRLCK, Whence: 0, Start: 0, Len: 0}
			err = unix.FcntlFlock(f.Fd(), unix.F_SETLK, &lock)
		}
		if err != nil {
			t.Fatal(err)
		}
		fmt.Println("LOCK_READY")
		var one [1]byte
		_, _ = os.Stdin.Read(one[:])
		return
	}
	for _, scheme := range []string{"fcntl", "flock"} {
		t.Run(scheme, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "source.db")
			if err := os.WriteFile(src, []byte("abc"), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLockedSource$")
			cmd.Env = append(os.Environ(), "CONVERTER_LOCK_CHILD="+src, "CONVERTER_LOCK_SCHEME="+scheme)
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { in.Close(); _ = cmd.Wait() }()
			var ready [11]byte
			if _, err = io.ReadFull(out, ready[:]); err != nil {
				t.Fatal(err)
			}
			if _, err = snapshot(src); err == nil || !strings.Contains(err.Error(), "in use") {
				t.Fatalf("exclusive lock not detected: %v", err)
			}
		})
	}
}

func TestCLI(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"--check"}, &out); err == nil {
		t.Fatal("missing path accepted")
	}
	src, _, _ := fixture(t, "Turso WAL")
	if err := run(context.Background(), []string{"--check", src}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Turso WAL") {
		t.Fatal(out.String())
	}
	out.Reset()
	dst := filepath.Join(t.TempDir(), "dst.db")
	if err := run(context.Background(), []string{src, dst}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "10005 rows") {
		t.Fatal(out.String())
	}
}

func TestCompatibleWALWithoutMarker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "wal.db")
	db, err := sql.Open("turso", p)
	if err != nil {
		t.Fatal(err)
	}
	execFixture(t, db, "CREATE TABLE rows(id INTEGER PRIMARY KEY)")
	execFixture(t, db, "INSERT INTO rows VALUES(1)")
	execFixture(t, db, "PRAGMA wal_checkpoint(TRUNCATE)")
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := check(context.Background(), p)
	if err != nil || got != "plain SQLite" {
		t.Fatalf("compatible unmarked WAL %q %v", got, err)
	}
}

func TestTruncatedValidHeader(t *testing.T) {
	p := filepath.Join(t.TempDir(), "short.db")
	b := make([]byte, 100)
	copy(b, "SQLite format 3\x00")
	binary.BigEndian.PutUint16(b[16:18], 4096)
	b[18] = 255
	b[19] = 255
	if err := os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := convert(context.Background(), p, p+".out"); err == nil {
		t.Fatal("accepted truncated page")
	}
}

func assertNoStaging(t *testing.T, dst string) {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(filepath.Dir(dst), ".turso-convert-*"))
	if err != nil || len(paths) != 0 {
		t.Fatalf("staging files remain: %v %v", paths, err)
	}
}

func TestSamePathAndSymlinkRefused(t *testing.T) {
	src, _, _ := fixture(t, "plain rollback")
	before := fileHashes(t, src)
	if _, err := convert(context.Background(), src, src); err == nil {
		t.Fatal("accepted in-place conversion")
	}
	dst := filepath.Join(t.TempDir(), "symlink.db")
	if err := os.Symlink(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := convert(context.Background(), src, dst); err == nil {
		t.Fatal("accepted symlink destination")
	}
	if !reflect.DeepEqual(before, fileHashes(t, src)) {
		t.Fatal("source changed")
	}
	assertNoStaging(t, dst)
}

func TestEmptyAUTOINCREMENT(t *testing.T) {
	for _, kind := range []string{"Turso WAL", "Turso MVCC"} {
		t.Run(kind, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "empty.db")
			db, err := sql.Open("turso", src)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "Turso MVCC" {
				execFixture(t, db, "PRAGMA journal_mode=mvcc")
			}
			execFixture(t, db, "CREATE TABLE empty(id INTEGER PRIMARY KEY AUTOINCREMENT,v TEXT)")
			execFixture(t, db, "PRAGMA wal_checkpoint(TRUNCATE)")
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			dst := src + ".converted"
			if _, err = convert(context.Background(), src, dst); err != nil {
				t.Fatal(err)
			}
			db, err = sql.Open("sqlite", dst)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			execFixture(t, db, "INSERT INTO empty(v) VALUES('first')")
			var id int64
			if err = db.QueryRow("SELECT id FROM empty").Scan(&id); err != nil || id != 1 {
				t.Fatalf("first id %d want 1: %v", id, err)
			}
		})
	}
}

func TestRelativePaths(t *testing.T) {
	src, _, _ := fixture(t, "plain rollback")
	t.Chdir(filepath.Dir(src))
	var out bytes.Buffer
	if err := run(context.Background(), []string{"source.db", "output.db"}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat("output.db"); err != nil {
		t.Fatal(err)
	}
}
