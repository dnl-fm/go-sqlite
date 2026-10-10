package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	_ "turso.tech/database/tursogo"
)

type options struct {
	probe, db, out                  string
	label                           string
	rows                            int64
	mvccGC, mvccCheckpoint          int64
	sizeMB, passes, conns, cacheKiB int
	trim                            bool
}
type recorder struct {
	file *os.File
	csv  *csv.Writer
}
type measurement struct {
	rss  int64
	heap uint64
}

func main() {
	o := options{}
	flag.StringVar(&o.probe, "probe", "", "probe name")
	flag.StringVar(&o.db, "db", "lab.db", "database path")
	flag.StringVar(&o.out, "out", ".", "output directory")
	flag.StringVar(&o.label, "label", "", "gosqlite-steady evidence label")
	flag.Int64Var(&o.mvccGC, "mvcc-gc-threshold", -1, "gosqlite-steady GC threshold (unset at -1)")
	flag.Int64Var(&o.mvccCheckpoint, "mvcc-checkpoint-threshold", -1, "gosqlite-steady checkpoint bytes (unset at -1)")
	flag.Int64Var(&o.rows, "rows", 300000, "sample rows, a positive multiple of 40 (600000 for steady/GC/reader probes)")
	flag.BoolVar(&o.trim, "trim", false, "sample after glibc malloc_trim for steady/GC and vacuum probes")
	flag.IntVar(&o.sizeMB, "size-mb", 150, "approximate database size in MiB")
	flag.IntVar(&o.passes, "passes", 5, "VACUUM INTO passes")
	flag.IntVar(&o.conns, "conns", 4, "cache probe connections")
	flag.IntVar(&o.cacheKiB, "cache-kib", 2048, "cache limit per connection in KiB")
	flag.Parse()
	rowsSet := false
	flag.Visit(func(f *flag.Flag) { rowsSet = rowsSet || f.Name == "rows" })
	if !rowsSet {
		o.rows = defaultRows(o.probe)
	}
	if err := os.MkdirAll(o.out, 0755); err != nil {
		fail(err)
	}
	if o.probe == "verdict-growth" {
		if err := growthVerdicts(o.out); err != nil {
			fail(err)
		}
		return
	}
	verdict, metrics, threshold, err := run(o)
	if err != nil {
		verdict = "ERROR"
		metrics = "message=" + err.Error()
	}
	if writeErr := summary(o.out, o.evidenceName(), verdict, metrics, threshold); writeErr != nil {
		err = errors.Join(err, writeErr)
	}
	if err != nil {
		fail(err)
	}
}
func defaultRows(probe string) int64 {
	switch probe {
	case "mvcc-steady", "wal-steady", "mvcc-forced-gc", "mvcc-open-reader", "gosqlite-steady":
		return 600000
	default:
		return 300000
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
func (o options) evidenceName() string {
	if o.probe == "gosqlite-steady" && o.label != "" {
		return o.probe + "-" + o.label
	}
	return o.probe
}
func run(o options) (verdict, metrics, threshold string, runErr error) {
	if o.label != "" && (o.probe != "gosqlite-steady" || strings.ContainsAny(o.label, "/\\") || o.label == "." || o.label == "..") {
		return "", "", "", errors.New("-label must be a filename label for gosqlite-steady")
	}
	switch o.probe {
	case "mvcc-growth", "wal-growth", "mvcc-growth-checkpoint", "mvcc-growth-1conn", "mvcc-steady", "wal-steady", "mvcc-forced-gc", "mvcc-open-reader", "gosqlite-steady", "build", "vacuum-into", "cache-size":
	default:
		return "", "", "", fmt.Errorf("unknown probe %q", o.probe)
	}
	f, err := os.Create(filepath.Join(o.out, o.evidenceName()+".csv"))
	if err != nil {
		return "", "", "", err
	}
	r := &recorder{file: f, csv: csv.NewWriter(f)}
	defer func() { r.csv.Flush(); runErr = errors.Join(runErr, r.csv.Error(), f.Close()) }()
	if err := r.csv.Write([]string{"phase", "rows", "pass", "connections_read", "rss_anon_kib", "go_heap_inuse_kib", "detail"}); err != nil {
		return "", "", "", err
	}
	switch o.probe {
	case "build":
		return build(o, r)
	case "vacuum-into":
		return vacuum(o, r)
	case "cache-size":
		return cache(o, r)
	case "mvcc-steady", "wal-steady", "mvcc-forced-gc", "mvcc-open-reader", "gosqlite-steady":
		return steady(o, r)
	default:
		return growth(o, r)
	}
}
func (r *recorder) sample(phase string, rows int64, pass, conns int, detail string) (measurement, error) {
	runtime.GC()
	debug.FreeOSMemory()
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return measurement{}, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) == 3 && fields[0] == "RssAnon:" && fields[2] == "kB" {
			rss, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil {
				return measurement{}, err
			}
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			m := measurement{rss: rss, heap: stats.HeapInuse / 1024}
			err = r.csv.Write([]string{phase, strconv.FormatInt(rows, 10), strconv.Itoa(pass), strconv.Itoa(conns), strconv.FormatInt(m.rss, 10), strconv.FormatUint(m.heap, 10), detail})
			r.csv.Flush()
			if err == nil {
				err = r.csv.Error()
			}
			return m, err
		}
	}
	return measurement{}, errors.Join(errors.New("RssAnon missing from /proc/self/status"), s.Err())
}
func summary(out, probe, verdict, metrics, threshold string) error {
	clean := func(s string) string { return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s) }
	f, err := os.OpenFile(filepath.Join(out, "summary.tsv"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\t%s\t%s\t%s\n", clean(probe), verdict, clean(metrics), clean(threshold))
	return errors.Join(err, f.Close())
}
func openPool(path, mode string, n int) (*sql.DB, []*sql.Conn, error) {
	db, err := sql.Open("turso", path)
	if err != nil {
		return nil, nil, err
	}
	db.SetMaxOpenConns(n)
	db.SetMaxIdleConns(n)
	var conns []*sql.Conn
	for i := 0; i < n; i++ {
		c, e := db.Conn(context.Background())
		if e == nil {
			conns = append(conns, c)
			e = configure(c, mode)
		}
		if e != nil {
			closePool(db, conns)
			return nil, nil, fmt.Errorf("connection %d: %w", i+1, e)
		}
	}
	return db, conns, nil
}
func configure(c *sql.Conn, mode string) error {
	ctx := context.Background()
	for _, q := range []string{"PRAGMA busy_timeout = 5000", "PRAGMA journal_mode = '" + mode + "'"} {
		if _, err := c.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("%s: %w", q, err)
		}
	}
	var actual string
	if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&actual); err != nil {
		return err
	}
	if !strings.EqualFold(actual, mode) {
		return fmt.Errorf("journal_mode=%q, want %s", actual, mode)
	}
	return nil
}
func closePool(db *sql.DB, conns []*sql.Conn) error {
	var err error
	for _, c := range conns {
		err = errors.Join(err, c.Close())
	}
	return errors.Join(err, db.Close())
}
func reproduced(yes bool) string {
	if yes {
		return "REPRODUCED"
	}
	return "NOT_REPRODUCED"
}
