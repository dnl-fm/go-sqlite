package main

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultRows(t *testing.T) {
	for _, probe := range []string{"mvcc-steady", "wal-steady", "mvcc-forced-gc", "mvcc-open-reader"} {
		if got := defaultRows(probe); got != 600000 {
			t.Errorf("%s default rows=%d, want 600000", probe, got)
		}
	}
	for _, probe := range []string{"mvcc-growth", "wal-growth", "mvcc-growth-checkpoint", "mvcc-growth-1conn", "vacuum-into", "cache-size"} {
		if got := defaultRows(probe); got != 300000 {
			t.Errorf("%s default rows=%d, want 300000", probe, got)
		}
	}
}

func TestTrimSample(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trim.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	r := &recorder{file: f, csv: csv.NewWriter(f)}
	m, err := r.trimSample("trim_phase", 123, 2, 1, "journal_mode=mvcc")
	closeErr := f.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	f, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	record, err := csv.NewReader(f).Read()
	if err != nil {
		t.Fatal(err)
	}
	if m.rss <= 0 || len(record) != 7 || record[0] != "trim_phase" || record[1] != "123" || record[2] != "2" || record[3] != "1" || record[4] != strconv.FormatInt(m.rss, 10) {
		t.Fatalf("invalid sample measurement=%+v csv=%v", m, record)
	}
	if !strings.HasPrefix(record[6], "journal_mode=mvcc malloc_trim_result=") || (record[6] != "journal_mode=mvcc malloc_trim_result=0" && record[6] != "journal_mode=mvcc malloc_trim_result=1") {
		t.Fatalf("missing allocator result: %q", record[6])
	}
}
