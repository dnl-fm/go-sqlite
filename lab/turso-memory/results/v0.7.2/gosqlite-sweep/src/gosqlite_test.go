package main

import (
	"context"
	"database/sql"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dnl-fm/go-sqlite/pkg/database"
)

func TestGoSQLiteConfigOverridesOnlyRequestedThresholds(t *testing.T) {
	for _, tt := range []struct {
		name           string
		gc, checkpoint int64
		wantGC, wantCP string
	}{
		{"defaults", -1, -1, "", ""},
		{"mild", 4096, 1048576, "4096", "1048576"},
		{"medium", 1024, 262144, "1024", "262144"},
		{"eager", 1, 0, "1", "0"},
		{"GC only", 1024, -1, "1024", ""},
		{"checkpoint only", -1, 0, "", "0"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := goSQLiteConfig(options{mvccGC: tt.gc, mvccCheckpoint: tt.checkpoint})
			if err != nil {
				t.Fatal(err)
			}
			defaults := database.DefaultConfig()
			if cfg.MaxOpenConns != defaults.MaxOpenConns || cfg.MaxIdleConns != defaults.MaxIdleConns {
				t.Fatalf("pool configuration changed: %+v", cfg)
			}
			for key, want := range defaults.Pragmas {
				if cfg.Pragmas[key] != want {
					t.Errorf("default %s=%s, want %s", key, cfg.Pragmas[key], want)
				}
			}
			if cfg.Pragmas["mvcc_gc_threshold"] != tt.wantGC || cfg.Pragmas["mvcc_checkpoint_threshold"] != tt.wantCP {
				t.Fatalf("unexpected thresholds: %+v", cfg.Pragmas)
			}
		})
	}
	for _, pair := range [][2]int64{{0, -1}, {-2, -1}, {-1, -2}} {
		if _, err := goSQLiteConfig(options{mvccGC: pair[0], mvccCheckpoint: pair[1]}); err == nil {
			t.Errorf("accepted thresholds %v", pair)
		}
	}
}

func TestGoSQLiteReadbacksReportEachConnectionAndRejectMismatches(t *testing.T) {
	for _, tt := range []struct {
		name, failSQL, wantErr string
		foreignKeys            int64
	}{
		{"configured", "", "", 1},
		{"unconfigured pooled connection", "", "foreign_keys=0, want 1", 0},
		{"readback rejected", "PRAGMA mvcc_gc_threshold", "readback mvcc_gc_threshold", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := &steadyPragmaConn{failSQL: tt.failSQL, values: map[string]int64{
				"foreign_keys": tt.foreignKeys, "busy_timeout": 1000, "cache_size": -64000,
				"mvcc_gc_threshold": 16384, "mvcc_checkpoint_threshold": 4120000,
			}}
			db := sql.OpenDB(steadyPragmaConnector{stub})
			defer db.Close()
			c, err := db.Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			got, err := goSQLiteReadbacks(context.Background(), c, database.DefaultConfig(), 4)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error=%v, want %s", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for name := range stub.values {
				if !strings.Contains(got, "conn4_"+name+"=") {
					t.Errorf("missing connection readback for %s: %s", name, got)
				}
			}
		})
	}
}

func TestGoSQLitePoolRecordsFourConfiguredConnections(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "readbacks.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := &recorder{file: f, csv: csv.NewWriter(f)}
	db, conns, got, err := openGoSQLitePool(options{db: filepath.Join(dir, "pool.db"), mvccGC: 1, mvccCheckpoint: 0}, r)
	if err != nil {
		t.Fatal(err)
	}
	defer closePool(db, conns)
	if len(conns) != 4 || db.Stats().InUse != 4 {
		t.Fatalf("connections=%d in_use=%d, want 4", len(conns), db.Stats().InUse)
	}
	for _, number := range []string{"1", "2", "3", "4"} {
		for _, readback := range []string{"foreign_keys=1", "busy_timeout=1000", "cache_size=-64000", "mvcc_gc_threshold=1", "mvcc_checkpoint_threshold=0"} {
			if !strings.Contains(got, "conn"+number+"_"+readback) {
				t.Errorf("missing conn%s_%s: %s", number, readback, got)
			}
		}
	}
	contents, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(contents), "pragma_readback,") != 4 {
		t.Fatalf("CSV lacks four readback samples: %s", contents)
	}
}

func TestGoSQLiteProbeDefaultsAndEvidenceNames(t *testing.T) {
	if defaultRows("gosqlite-steady") != 600000 {
		t.Fatal("go-sqlite probe must default to 600k inserts")
	}
	if got := (options{probe: "gosqlite-steady", label: "medium-arena2"}).evidenceName(); got != "gosqlite-steady-medium-arena2" {
		t.Fatal(got)
	}
	for _, label := range []string{"../escape", "a/b", "a\\b", ".", ".."} {
		if _, _, _, err := run(options{probe: "gosqlite-steady", label: label}); err == nil {
			t.Errorf("accepted unsafe label %q", label)
		}
	}
}
