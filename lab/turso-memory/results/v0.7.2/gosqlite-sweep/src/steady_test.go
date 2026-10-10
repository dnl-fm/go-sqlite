package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestSteadyVerdictUsesRawMeasurements(t *testing.T) {
	for _, tt := range []struct {
		name              string
		halfRows, endRows int64
		halfRSS, endRSS   int64
		want              string
	}{
		{"slope rounds up to 400", 0, 256000, 100001, 200000, "FLAT"},
		{"inclusive 400 boundary", 0, 256000, 100000, 200000, "GROWS"},
		{"second half only", 256000, 512000, 100000, 200000, "GROWS"},
		{"memory decreases", 0, 256000, 200000, 100000, "FLAT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, bpr, err := steadyVerdict(tt.halfRows, tt.endRows, tt.halfRSS, tt.endRSS)
			if err != nil || got != tt.want {
				t.Fatalf("verdict=%s bpr=%v error=%v, want %s", got, bpr, err, tt.want)
			}
			if tt.name == "slope rounds up to 400" && fmt.Sprintf("%.2f", bpr) != "400.00" {
				t.Fatalf("test does not exercise rounded boundary: %.2f", bpr)
			}
		})
	}
}

func TestSteadyVerdictRejectsInvalidMeasurements(t *testing.T) {
	for _, values := range [][4]int64{{-1, 40, 1, 1}, {40, 40, 1, 1}, {80, 40, 1, 1}, {0, 40, -1, 1}, {0, 40, 1, 0}} {
		if _, _, err := steadyVerdict(values[0], values[1], values[2], values[3]); err == nil {
			t.Errorf("accepted invalid measurements %v", values)
		}
	}
}

func TestSteadyCountsMatchExclusiveRetentionCutoff(t *testing.T) {
	for _, tt := range []struct {
		ticks, samples, deleted int64
	}{
		{0, 0, 0},
		{500, 20000, 0},
		{749, 29960, 0},
		{750, 20040, 9960},
		{999, 30000, 9960},
		{1000, 20040, 19960},
		{15000, 20040, 579960},
		{15250, 20040, 589960},
	} {
		if err := checkSteadyCounts(tt.ticks, tt.samples, tt.ticks, tt.deleted, false); err != nil {
			t.Errorf("ticks=%d: %v", tt.ticks, err)
		}
	}
}

func TestSteadyCountsRejectWorkloadErrors(t *testing.T) {
	for _, tt := range []struct {
		name                            string
		ticks, samples, events, deleted int64
	}{
		{"no retention", 500, 20000, 500, 0},
		{"inclusive cutoff error", 750, 20000, 750, 10000},
		{"missing samples", 750, 20039, 750, 9960},
		{"event cleanup", 750, 20040, 501, 9960},
		{"delete accounting error", 750, 20040, 750, 9959},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := checkSteadyCounts(tt.ticks, tt.samples, tt.events, tt.deleted, true); err == nil {
				t.Fatal("accepted incorrect workload")
			}
		})
	}
}

func TestSteadyRejectsInvalidRowsBeforeOpeningDatabase(t *testing.T) {
	for _, rows := range []int64{-40, 0, 41} {
		if _, _, _, err := steady(options{rows: rows}, nil); err == nil || !strings.Contains(err.Error(), "-rows") {
			t.Errorf("rows=%d: %v", rows, err)
		}
	}
}

func TestSteadyPragmasSetAndVerifyForcedControls(t *testing.T) {
	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		forced  bool
		values  map[string]int64
		failSQL string
		wantErr string
	}{
		{"forced", true, map[string]int64{"mvcc_checkpoint_threshold": 0, "mvcc_gc_threshold": 1}, "", ""},
		{"defaults", false, map[string]int64{"mvcc_checkpoint_threshold": 4120000, "mvcc_gc_threshold": 16384}, "", ""},
		{"checkpoint write rejected", true, nil, "PRAGMA mvcc_checkpoint_threshold = 0", "forced GC PRAGMA mvcc_checkpoint_threshold"},
		{"GC write rejected", true, map[string]int64{"mvcc_checkpoint_threshold": 0}, "PRAGMA mvcc_gc_threshold = 1", "forced GC PRAGMA mvcc_gc_threshold"},
		{"checkpoint read mismatch", true, map[string]int64{"mvcc_checkpoint_threshold": 1}, "", "mvcc_checkpoint_threshold=1, want 0"},
		{"GC read mismatch", true, map[string]int64{"mvcc_checkpoint_threshold": 0, "mvcc_gc_threshold": 0}, "", "mvcc_gc_threshold=0, want 1"},
		{"forced read rejected", true, nil, "PRAGMA mvcc_checkpoint_threshold", "MVCC readback mvcc_checkpoint_threshold"},
		{"default read rejected", false, nil, "PRAGMA mvcc_checkpoint_threshold", "MVCC readback mvcc_checkpoint_threshold"},
		{"unknown forced pragma", true, map[string]int64{"mvcc_checkpoint_threshold": 0}, "", "MVCC readback mvcc_gc_threshold"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stub := &steadyPragmaConn{values: tt.values, failSQL: tt.failSQL}
			db := sql.OpenDB(steadyPragmaConnector{stub})
			defer db.Close()
			c, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			got, err := steadyPragmas(ctx, c, tt.forced)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error=%v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for name, value := range tt.values {
				if !strings.Contains(got, fmt.Sprintf("%s=%d", name, value)) {
					t.Errorf("missing readback %s in %q", name, got)
				}
			}
			writes := strings.Join(stub.writes, ";")
			if tt.forced && writes != "PRAGMA mvcc_checkpoint_threshold = 0;PRAGMA mvcc_gc_threshold = 1" {
				t.Errorf("forced writes=%q", writes)
			}
			if !tt.forced && len(stub.writes) != 0 {
				t.Errorf("default controls were changed: %q", writes)
			}
		})
	}
}

// A narrow SQL driver makes rejected/ignored pragmas deterministic without
// depending on the native version linked into this test process.
type steadyPragmaConnector struct{ conn *steadyPragmaConn }

func (s steadyPragmaConnector) Connect(context.Context) (driver.Conn, error) { return s.conn, nil }
func (s steadyPragmaConnector) Driver() driver.Driver                        { return steadyPragmaDriver{} }

type steadyPragmaDriver struct{}

func (steadyPragmaDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused Open") }

type steadyPragmaConn struct {
	values  map[string]int64
	failSQL string
	writes  []string
}

func (*steadyPragmaConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unused Prepare")
}
func (*steadyPragmaConn) Begin() (driver.Tx, error) { return nil, errors.New("unused Begin") }
func (*steadyPragmaConn) Close() error              { return nil }
func (s *steadyPragmaConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	s.writes = append(s.writes, query)
	if query == s.failSQL {
		return nil, errors.New("pragma rejected")
	}
	return driver.RowsAffected(0), nil
}
func (s *steadyPragmaConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query == s.failSQL {
		return nil, errors.New("pragma rejected")
	}
	value, ok := s.values[strings.TrimPrefix(query, "PRAGMA ")]
	return &steadyPragmaRows{value: value, done: !ok}, nil
}

type steadyPragmaRows struct {
	value int64
	done  bool
}

func (*steadyPragmaRows) Columns() []string { return []string{"value"} }
func (*steadyPragmaRows) Close() error      { return nil }
func (r *steadyPragmaRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	values[0], r.done = r.value, true
	return nil
}
