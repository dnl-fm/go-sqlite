package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrowthVerdictsUseRawMeasurements(t *testing.T) {
	for _, tt := range []struct {
		name                                       string
		rowsHalf, rowsEnd, rssHalf, rssEnd, walRSS int64
		verdict                                    string
	}{
		{"slope rounds up to 400", 0, 256000, 100001, 200000, 40000, "NOT_REPRODUCED"},
		{"RSS ratio rounds up to four", 0, 256000, 100000, 409687, 102422, "NOT_REPRODUCED"},
		{"exact inclusive thresholds", 0, 256000, 309600, 409600, 102400, "REPRODUCED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := t.TempDir()
			// Include the same rounded presentation fields emitted by the probes.
			bpr := float64(tt.rssEnd-tt.rssHalf) * 1024 / float64(tt.rowsEnd-tt.rowsHalf)
			metrics := fmt.Sprintf("bytes_per_row=%.2f rss_end_mib=%.3f rows_half=%d rows_end=%d rss_half_kib=%d rss_end_kib=%d", bpr, float64(tt.rssEnd)/1024, tt.rowsHalf, tt.rowsEnd, tt.rssHalf, tt.rssEnd)
			for _, probe := range []string{"mvcc-growth", "mvcc-growth-checkpoint", "mvcc-growth-1conn"} {
				if err := summary(out, probe, "REPRODUCED", metrics, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := summary(out, "wal-growth", "NOT_REPRODUCED", fmt.Sprintf("rss_end_mib=%.3f rss_end_kib=%d", float64(tt.walRSS)/1024, tt.walRSS), ""); err != nil {
				t.Fatal(err)
			}
			if err := growthVerdicts(out); err != nil {
				t.Fatal(err)
			}
			assertRetentionVerdicts(t, out, tt.verdict)
		})
	}
}

func TestGrowthVerdictsRejectMissingOrFailedMeasurements(t *testing.T) {
	for _, tt := range []struct{ name, walVerdict, walMetrics, mvccMetrics string }{
		{"missing WAL control", "", "", "rows_half=0 rows_end=256000 rss_half_kib=100000 rss_end_kib=300000"},
		{"failed WAL control", "ERROR", "rss_end_kib=10000", "rows_half=0 rows_end=256000 rss_half_kib=100000 rss_end_kib=300000"},
		{"missing raw RSS", "NOT_REPRODUCED", "rss_end_mib=10.0", "rows_half=0 rows_end=256000 rss_half_kib=100000 rss_end_kib=300000"},
		{"invalid row interval", "NOT_REPRODUCED", "rss_end_kib=10000", "rows_half=100 rows_end=100 rss_half_kib=100000 rss_end_kib=300000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := t.TempDir()
			for _, probe := range []string{"mvcc-growth", "mvcc-growth-checkpoint", "mvcc-growth-1conn"} {
				if err := summary(out, probe, "REPRODUCED", tt.mvccMetrics, ""); err != nil {
					t.Fatal(err)
				}
			}
			if tt.walVerdict != "" {
				if err := summary(out, "wal-growth", tt.walVerdict, tt.walMetrics, ""); err != nil {
					t.Fatal(err)
				}
			}
			if err := growthVerdicts(out); err != nil {
				t.Fatal(err)
			}
			assertRetentionVerdicts(t, out, "ERROR")
		})
	}
}

func assertRetentionVerdicts(t *testing.T, out, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "summary.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, probe := range []string{"mvcc-retention", "mvcc-retention-checkpoint", "mvcc-retention-1conn"} {
		found := false
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Split(line, "\t")
			if len(fields) >= 2 && fields[0] == probe {
				found = true
				if fields[1] != want {
					t.Errorf("%s verdict=%s, want %s", probe, fields[1], want)
				}
			}
		}
		if !found {
			t.Errorf("missing %s", probe)
		}
	}
}
