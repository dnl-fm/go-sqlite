package writers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain writes the measurements of a full run to results/. Short runs and
// the child processes of the multi-process strategy write nothing.
func TestMain(m *testing.M) {
	started := time.Now()
	code := m.Run()
	// A filtered run of a few cells must not replace the record of a whole one.
	if fullMode() && os.Getenv(childEnv) == "" && len(results) > 0 && len(results) == scenarioCells {
		err := writeResults(started, time.Now(), code)
		if err != nil {
			fmt.Fprintln(os.Stderr, "write results:", err)
			code = 1
		}
	}
	// A filtered run of a few cells must not replace the record of a whole one.
	if fullMode() && len(matrixResults) > 0 && len(matrixResults) == len(matrixCells()) {
		err := writeMatrixResults(started)
		if err != nil {
			fmt.Fprintln(os.Stderr, "write wal-matrix results:", err)
			code = 1
		}
	}
	if fullMode() && completeLoadResults(loadResults) {
		if err := writeLoadResults(started); err != nil {
			fmt.Fprintln(os.Stderr, "write load results:", err)
			code = 1
		}
	}
	os.Exit(code)
}

func writeResults(started, ended time.Time, code int) error {
	err := os.MkdirAll("results", 0o755)
	if err != nil {
		return err
	}
	date := started.Format("2006-01-02")

	var tsv strings.Builder
	tsv.WriteString("workload\tstrategy\tops\tbusy\tother\tlost\tops_per_s\twrite_p50_ms\twrite_p99_ms\twrite_max_ms\tread_p50_ms\tread_p99_ms\twal_mb\tfirst_busy\tfirst_other\n")
	for _, r := range results {
		fmt.Fprintf(&tsv, "%s\t%s\t%d\t%d\t%d\t%d\t%.0f\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Workload, r.Strategy, r.Ops, r.Busy, r.Other, r.Lost, r.opsPerSec(),
			ms(r.WP50), ms(r.WP99), ms(r.WMax), ms(r.RP50), ms(r.RP99), mb(r.WALBytes), r.FirstBusy, r.FirstOther)
	}
	err = os.WriteFile(filepath.Join("results", date+".tsv"), []byte(tsv.String()), 0o644)
	if err != nil {
		return err
	}

	if len(walResults) > 0 {
		var wal strings.Builder
		wal.WriteString("variant\tsecond\twal_mb\n")
		for _, r := range walResults {
			for i, size := range r.Series {
				fmt.Fprintf(&wal, "%s\t%d\t%s\n", r.Name, i+1, mb(size))
			}
		}
		err = os.WriteFile(filepath.Join("results", date+"-wal.tsv"), []byte(wal.String()), 0o644)
		if err != nil {
			return err
		}
	}

	var md strings.Builder
	fmt.Fprintf(&md, "# sqlite-writers: full run %s\n\n", date)
	fmt.Fprintf(&md, "- started %s, ended %s (%s wall clock), test exit code %d\n",
		started.Format(time.RFC3339), ended.Format(time.RFC3339), ended.Sub(started).Round(time.Second), code)
	fmt.Fprintf(&md, "- %s, %s/%s, GOMAXPROCS %d, %s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.GOMAXPROCS(0), engine())
	inserts, counter, longTx := sizes()
	fmt.Fprintf(&md, "- %d writers, %d readers pausing %s between reads; inserts %d/writer (FULL rows %d/writer), counter %d/writer, long-tx %s with %d inserters behind a %s transaction\n",
		scenarioWriters, scenarioReaders, scenarioReadPause, inserts, inserts/20, counter, longTx, longTxInserters, longTxHold)
	md.WriteString("- other labs were benchmarking on the same machine at the same time; treat the numbers as ratios, not absolutes\n")
	md.WriteString("\nLatencies in milliseconds. `lost` = acknowledged writes missing from the database. `wal MB` = size of the `-wal` file when the writers finished.\n\n")
	md.WriteString(table(results))
	for _, r := range results {
		if r.FirstBusy != "" {
			fmt.Fprintf(&md, "\n%s / %s, first of %d busy errors: `%s`\n", r.Workload, r.Strategy, r.Busy, r.FirstBusy)
		}
		if r.FirstOther != "" {
			fmt.Fprintf(&md, "\n%s / %s, first of %d other errors: `%s`\n", r.Workload, r.Strategy, r.Other, r.FirstOther)
		}
	}
	if len(walResults) > 0 {
		fmt.Fprintf(&md, "\n## wal-growth\n\n%s per variant, %d writers pausing %s between commits, %d readers. Sizes in MB.\n\n",
			walDuration, walWriters, walWritePause, scenarioReaders)
		md.WriteString(walTable(walResults))
	}
	// Keep independently measured sections; their own runs replace them.
	old, _ := os.ReadFile(filepath.Join("results", "latest.md"))
	for _, heading := range []string{"\n## wal-matrix\n", "\n## load\n"} {
		if _, section, found := strings.Cut(string(old), heading); found {
			if i := strings.Index(section, "\n## "); i >= 0 {
				section = section[:i]
			}
			md.WriteString(heading + section)
		}
	}
	return os.WriteFile(filepath.Join("results", "latest.md"), []byte(md.String()), 0o644)
}

// engine names the driver and SQLite versions the numbers were measured on.
// The driver version comes from go.mod: a test binary carries no dependency
// list in its build info.
func engine() string {
	driver := "modernc.org/sqlite (version unknown)"
	mod, _ := os.ReadFile("go.mod")
	for line := range strings.Lines(string(mod)) {
		if fields := strings.Fields(line); len(fields) >= 2 && fields[len(fields)-2] == "modernc.org/sqlite" {
			driver = "modernc.org/sqlite " + fields[len(fields)-1]
		}
	}
	version := "unknown"
	store, err := Open(context.Background(), filepath.Join(os.TempDir(), fmt.Sprintf("sqlite-writers-version-%d.db", os.Getpid())), Options{})
	if err == nil {
		_ = store.Reader.QueryRow("SELECT sqlite_version()").Scan(&version)
		_ = store.Close()
	}
	return driver + ", SQLite " + version
}
