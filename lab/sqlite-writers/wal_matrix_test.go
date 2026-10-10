package writers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// The wal-matrix load. Every cell is offered the same write rate, so commit
// counts and WAL sizes compare across cells; only the number of writer
// connections and what the readers do changes.
const (
	matrixDuration        = 30 * time.Second
	matrixWriters         = 4    // goroutines, whatever the number of connections
	matrixRate            = 800  // commits per second offered, all writers together
	matrixPayload         = 2048 // bytes per row
	matrixReaders         = 8    // reader goroutines, and the size of the read-only pool
	matrixCheckpointEvery = 5 * time.Second
	matrixQuiet           = 50 * time.Millisecond // the quiet window given to Store.Checkpoint, by default
	walFrameBytes         = 4096 + 24             // one page plus its frame header
)

// matrixCell is one run against a fresh database.
type matrixCell struct {
	group, name string
	// conns is the number of writer connections: 1 is Open (pattern C), more is
	// a pool with _txlock=immediate (pattern B). Readers are on their own
	// read-only pool either way.
	conns int
	// Every read is one Store.Read transaction.
	// readers: "none"; "point" (a point lookup, then 1ms pause); "scan"
	// (count(*) and a point lookup, then 1ms pause; SQLite answers this
	// count(*) from the small UNIQUE index, so it stays fast); "longscan" (the
	// same with the count forced over the table itself, a read that gets
	// longer as the table grows); "overlap" (point lookups with no pause);
	// "pinned" (one read transaction held open for the whole cell, nothing
	// else).
	readers    string
	duration   time.Duration // default matrixDuration
	writePause time.Duration // > 0: each writer pauses this long instead of following the shared rate
	pragmas    []string      // extra pragmas on every writer connection
	checkpoint string        // every 5s: PRAGMA wal_checkpoint("RESTART" or "TRUNCATE") on the writer pool, or "Store.Checkpoint"
	quiet      time.Duration // Store.Checkpoint's quiet window, default matrixQuiet
	shared     bool          // readers and writers share one pool of 4, as in strategy B
	burst      bool          // this lab's original B inserts cell, run by runPlan
}

func matrixCells() []matrixCell {
	var cells []matrixCell
	for _, readers := range []string{"none", "point", "scan", "longscan", "overlap", "pinned"} {
		for _, conns := range []int{1, 4} {
			cells = append(cells, matrixCell{group: "matrix", name: matrixName(conns, readers), conns: conns, readers: readers})
		}
	}
	// Mitigations, on three cells that grew for three different reasons:
	// several writer connections, long reads, a read transaction left open.
	noAuto := []string{"wal_autocheckpoint=0"}
	for _, base := range []matrixCell{{conns: 4, readers: "point"}, {conns: 1, readers: "longscan"}, {conns: 1, readers: "pinned"}} {
		for _, m := range []matrixCell{
			{name: "journal_size_limit=16MiB", pragmas: []string{"journal_size_limit=16777216"}},
			{name: "no auto-checkpoint, RESTART every 5s", pragmas: noAuto, checkpoint: "RESTART"},
			{name: "no auto-checkpoint, TRUNCATE every 5s", pragmas: noAuto, checkpoint: "TRUNCATE"},
			{name: "Store.Checkpoint every 5s", checkpoint: "Store.Checkpoint"},
			{name: "no auto-checkpoint, Store.Checkpoint every 5s", pragmas: noAuto, checkpoint: "Store.Checkpoint"},
		} {
			m.group, m.conns, m.readers = "mitigation", base.conns, base.readers
			m.name = matrixName(base.conns, base.readers) + " + " + m.name
			cells = append(cells, m)
		}
	}
	// The window has to outlast the reads: the same long reads, a window far
	// shorter than they are.
	cells = append(cells, matrixCell{
		group: "mitigation", name: matrixName(1, "longscan") + " + Store.Checkpoint every 5s, 5ms window",
		conns: 1, readers: "longscan", checkpoint: "Store.Checkpoint", quiet: 5 * time.Millisecond,
	})
	// The claims that contradicted each other, each with its build's own
	// parameters. The other build's schema is gone, so its count(*) runs both
	// ways: from this lab's index and over the table. It ran on tmpfs, where
	// fsync costs nothing; synchronous=OFF is that on a real disk.
	other := matrixCell{group: "claim", conns: 1, writePause: 4 * time.Millisecond, duration: 60 * time.Second}
	burst := matrixCell{group: "claim", name: "this lab: B immediate, inserts burst", conns: 4, readers: "scan", shared: true, burst: true}
	cScan, cLong, bLong, bFree := other, other, other, other
	cScan.name, cScan.readers = "other build: C split, scan readers", "scan"
	cLong.name, cLong.readers = "other build: C split, scan readers, count over the table", "longscan"
	bLong.name, bLong.readers, bLong.conns, bLong.shared = "other build: B immediate on one shared pool, count over the table", "longscan", 4, true
	bFree.name, bFree.readers, bFree.conns, bFree.shared = bLong.name+", synchronous=OFF", "longscan", 4, true
	bFree.pragmas = []string{"synchronous=OFF"} // the DSN's pragmas run in alphabetical order, so this one wins over NORMAL
	return append(cells, burst, cScan, cLong, bLong, bFree)
}

func matrixName(conns int, readers string) string {
	if conns == 1 {
		return "C 1 writer conn, readers " + readers
	}
	return fmt.Sprintf("B %d writer conns, readers %s", conns, readers)
}

// matrixRow is what one cell measured.
type matrixRow struct {
	matrixCell
	Commits, Errors, Lost  int
	FirstError             string
	Elapsed                time.Duration
	WP99, WMax, RP99, RMax time.Duration
	ReadOps                int
	OpenReads              float64 // mean number of reads in flight
	Series                 []int64 // -wal bytes, one sample per second
	RestartSeries          []uint32
	LagSeries              []int64 // WAL bytes not yet copied into the database
	Max, End               int64
	Restarts               uint32
	Checkpoints, Busy      int // timed checkpoints run, and how many reported busy
	CheckpointMax          time.Duration
	CheckpointError        string
}

var matrixResults []matrixRow

// walIndex reads what checkpoints have achieved from the wal-index header in
// the -shm file (https://sqlite.org/walformat.html#the_wal_index_file_format):
//
//   - salt-1, which SQLite increments by exactly one each time the WAL
//     restarts from frame 0, so the difference between two readings is the
//     number of restarts in between;
//   - mxFrame, the number of valid frames in the WAL;
//   - nBackfill, how many of those a checkpoint has copied into the database.
//
// It is a plain read of 100 bytes: no connection, no lock, nothing the
// workload can notice. The header is stored twice so that a reader can tell a
// half-written one; like SQLite, retry until both copies agree.
func walIndex(dbPath string) (salt, frames, backfilled uint32, err error) {
	f, err := os.Open(dbPath + "-shm")
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = f.Close() }()
	var b [100]byte
	for range 10 {
		_, err = f.ReadAt(b[:], 0)
		if err != nil {
			return 0, 0, 0, err
		}
		if bytes.Equal(b[:48], b[48:96]) {
			// The salt is a copy of the big-endian WAL header; the counters are
			// in the byte order of the machine.
			return binary.BigEndian.Uint32(b[32:]), binary.NativeEndian.Uint32(b[16:]), binary.NativeEndian.Uint32(b[96:]), nil
		}
	}
	return 0, 0, 0, errors.New("wal-index header kept changing")
}

// openMatrix opens the cell's store: Open for one writer connection, the same
// two pools with a wider writer pool otherwise.
func openMatrix(ctx context.Context, path string, c matrixCell) (*Store, error) {
	if c.shared {
		tg, err := openNaive(path, true, c.pragmas...) // one pool of 4 for readers and writers
		return &Store{Reader: tg.rdb, Writer: tg.wdb}, err
	}
	if c.conns == 1 {
		store, err := Open(ctx, path, Options{Readers: matrixReaders})
		if err != nil {
			return nil, err
		}
		for _, pragma := range c.pragmas {
			// The writer pool is one connection kept for life, so this reaches it.
			_, err = store.Writer.ExecContext(ctx, "PRAGMA "+pragma)
			if err != nil {
				_ = store.Close()
				return nil, err
			}
		}
		return store, nil
	}
	d := dsn(path, "NORMAL", false)
	for _, pragma := range c.pragmas {
		d += "&_pragma=" + pragma
	}
	writer, err := sql.Open("sqlite", d)
	if err != nil {
		return nil, err
	}
	writer.SetMaxOpenConns(c.conns)
	writer.SetMaxIdleConns(c.conns)
	err = writer.PingContext(ctx)
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader, err := sql.Open("sqlite", dsn(path, "NORMAL", true))
	if err != nil {
		_ = writer.Close()
		return nil, err
	}
	reader.SetMaxOpenConns(matrixReaders)
	reader.SetMaxIdleConns(matrixReaders)
	return &Store{Reader: reader, Writer: writer}, nil
}

// matrixLoad runs the cell's writers and readers until the cell's duration is
// over.
func matrixLoad(c matrixCell, store *Store, dbPath string) sample {
	ctx := context.Background()
	var (
		total            sample
		mu               sync.Mutex
		writers, readers sync.WaitGroup
		commits          atomic.Int64
	)
	finish := func(local sample) {
		mu.Lock()
		total.merge(local)
		mu.Unlock()
	}
	stop := make(chan struct{})
	body := strings.Repeat("x", matrixPayload)
	began := time.Now()
	deadline := began.Add(c.duration)

	// One ticker feeds all writers. A tick nobody is free to take is dropped,
	// so the offered rate is a ceiling and a stalled cell shows up as fewer
	// commits.
	var tick <-chan time.Time
	if c.writePause == 0 {
		ticker := time.NewTicker(time.Second / matrixRate)
		defer ticker.Stop()
		tick = ticker.C
	}
	for w := range matrixWriters {
		writers.Go(func() {
			var local sample
			defer func() { finish(local) }()
			for seq := 0; ; seq++ {
				if tick != nil {
					<-tick
				}
				if !time.Now().Before(deadline) {
					return
				}
				t0 := time.Now()
				err := store.Write(ctx, func(tx *sql.Tx) error {
					_, err := tx.Exec("INSERT INTO items (worker, seq, payload) VALUES (?, ?, ?)", w, seq, body)
					return err
				})
				if err != nil {
					local.fail("write", err)
					continue
				}
				local.Ops++
				commits.Add(1)
				local.WriteNS = append(local.WriteNS, int64(time.Since(t0)))
				if c.writePause > 0 {
					time.Sleep(c.writePause)
				}
			}
		})
	}

	n := matrixReaders
	if c.readers == "none" || c.readers == "pinned" {
		n = 0
	}
	for range n {
		readers.Go(func() {
			var local sample
			defer func() { finish(local) }()
			for {
				select {
				case <-stop:
					return
				default:
				}
				t0 := time.Now()
				err := store.Read(ctx, func(tx *sql.Tx) error {
					var v int64
					var err error
					switch c.readers {
					case "scan":
						err = tx.QueryRow("SELECT count(*) FROM items").Scan(&v)
					case "longscan":
						err = tx.QueryRow("SELECT count(*) FROM items NOT INDEXED").Scan(&v)
					}
					if err != nil {
						return err
					}
					// Rowids are handed out in commit order, so the newest
					// acknowledged row exists.
					err = tx.QueryRow("SELECT length(payload) FROM items WHERE id = ?", commits.Load()).Scan(&v)
					if errors.Is(err, sql.ErrNoRows) {
						return nil // nothing committed yet
					}
					return err
				})
				if err != nil {
					local.fail("read", err)
					continue
				}
				local.ReadNS = append(local.ReadNS, int64(time.Since(t0)))
				if c.readers != "overlap" {
					time.Sleep(time.Millisecond)
				}
			}
		})
	}

	writers.Wait()
	elapsed, wal := time.Since(began), walSize(dbPath)
	close(stop)
	readers.Wait()
	total.Elapsed, total.WALBytes = elapsed, wal
	return total
}

func runMatrixCell(t *testing.T, c matrixCell) matrixRow {
	t.Helper()
	ctx := context.Background()
	if c.duration == 0 {
		c.duration = matrixDuration
	}
	if c.quiet == 0 {
		c.quiet = matrixQuiet
	}
	dbPath := filepath.Join(labDir(t), "lab.db")
	store, err := openMatrix(ctx, dbPath, c)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	_, err = store.Writer.ExecContext(ctx, schema)
	if err != nil {
		t.Fatal(err)
	}
	r := matrixRow{matrixCell: c}

	if c.readers == "pinned" {
		held, err := store.Reader.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = held.Close() }()
		var n int
		_, err = held.ExecContext(ctx, "BEGIN")
		if err == nil {
			err = held.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&n) // the snapshot starts here
		}
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _, _ = held.ExecContext(ctx, "ROLLBACK") }()
	}

	base, frames, backfilled, err := walIndex(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	salt := base
	done := make(chan struct{})
	var watchers sync.WaitGroup
	watchers.Go(func() {
		second := time.NewTicker(time.Second)
		defer second.Stop()
		for {
			select {
			case <-done:
				return
			case <-second.C:
			}
			if s, f, b, err := walIndex(dbPath); err == nil {
				salt, frames, backfilled = s, f, b
			}
			r.Series = append(r.Series, walSize(dbPath))
			r.RestartSeries = append(r.RestartSeries, salt-base)
			r.LagSeries = append(r.LagSeries, int64(max(frames, backfilled)-backfilled)*walFrameBytes)
		}
	})
	if c.checkpoint != "" {
		watchers.Go(func() {
			every := time.NewTicker(matrixCheckpointEvery)
			defer every.Stop()
			for {
				select {
				case <-done:
					return
				case <-every.C:
				}
				var (
					busy              bool
					b, log, completed int
					err               error
				)
				t0 := time.Now()
				if c.checkpoint == "Store.Checkpoint" {
					busy, err = store.Checkpoint(ctx, c.quiet)
				} else {
					err = store.Writer.QueryRowContext(ctx, "PRAGMA wal_checkpoint("+c.checkpoint+")").Scan(&b, &log, &completed)
					busy = b != 0
				}
				r.CheckpointMax = max(r.CheckpointMax, time.Since(t0))
				r.Checkpoints++
				if err != nil && r.CheckpointError == "" {
					r.CheckpointError = err.Error()
				}
				if err != nil || busy {
					r.Busy++
				}
			}
		})
	}

	var s sample
	if c.burst {
		inserts, _, _ := sizes()
		tg := &target{write: store.Write, rdb: store.Reader, wdb: store.Writer}
		s = runPlan(plans("inserts", inserts, 1)[0], tg, dbPath)
	} else {
		s = matrixLoad(c, store, dbPath)
	}
	close(done)
	watchers.Wait()

	// With the writers stopped the -wal file's own header must carry the same
	// salt-1 (bytes 16-19) the restart count was taken from.
	salt, _, _, err = walIndex(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, 32)
	if f, err := os.Open(dbPath + "-wal"); err == nil {
		if _, err := f.ReadAt(header, 0); err == nil && binary.BigEndian.Uint32(header[16:]) != salt {
			t.Errorf("salt-1 is %d in the -wal header and %d in the wal-index", binary.BigEndian.Uint32(header[16:]), salt)
		}
		_ = f.Close()
	}

	slices.Sort(s.WriteNS)
	slices.Sort(s.ReadNS)
	var readNS int64
	for _, ns := range s.ReadNS {
		readNS += ns
	}
	r.Commits, r.Errors, r.Elapsed = s.Ops, s.Busy+s.Other, s.Elapsed
	r.FirstError = strings.TrimSpace(s.FirstBusy + " " + s.FirstOther)
	r.WP99, r.WMax = percentile(s.WriteNS, 0.99), percentile(s.WriteNS, 1)
	r.RP99, r.RMax = percentile(s.ReadNS, 0.99), percentile(s.ReadNS, 1)
	r.ReadOps, r.OpenReads = len(s.ReadNS), float64(readNS)/float64(s.Elapsed)
	r.End, r.Restarts = s.WALBytes, salt-base
	r.Max = max(slices.Max(append(r.Series, 0)), r.End)
	r.Lost = r.Commits - countItems(t, store.Reader)

	if r.Lost != 0 {
		t.Errorf("%d acknowledged writes are missing from the database", r.Lost)
	}
	if c.conns == 1 && r.Errors != 0 {
		t.Errorf("%d errors with one writer connection, want none (%s)", r.Errors, r.FirstError)
	}
	return r
}

// TestScenarioWALMatrix crosses the number of writer connections with what the
// readers do, at one fixed write rate, and counts WAL restarts. Only
// correctness is asserted; the WAL numbers are the findings.
func TestScenarioWALMatrix(t *testing.T) {
	if !fullMode() {
		t.Skip("wal-matrix runs in full mode only (LAB_FULL=1)")
	}
	for _, c := range matrixCells() {
		t.Run(c.group+"/"+c.name, func(t *testing.T) {
			r := runMatrixCell(t, c)
			matrixResults = append(matrixResults, r)
			t.Logf("%d commits, wal max %s MB, end %s MB, %d restarts", r.Commits, mb(r.Max), mb(r.End), r.Restarts)
		})
	}
	t.Log("\n" + matrixTable(matrixResults, ""))
}

// medianLag is the typical amount of WAL that no checkpoint had copied into
// the database yet. Near zero with a large WAL means checkpoints finish but
// the WAL cannot restart; near the WAL size means they cannot finish.
func (r matrixRow) medianLag() int64 {
	if len(r.LagSeries) == 0 {
		return 0
	}
	sorted := slices.Sorted(slices.Values(r.LagSeries))
	return sorted[len(sorted)/2]
}

// matrixTable renders the rows of one group ("" for all) as a markdown table.
// Sizes are MB, latencies milliseconds.
func matrixTable(rows []matrixRow, group string) string {
	var b strings.Builder
	b.WriteString("| cell | commits | errors | write p99 | write max | reads | read p99 | read max | open reads | wal @10s | @20s | wal max | wal end | restarts | not checkpointed | timed checkpoints |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		if group != "" && r.group != group {
			continue
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s | %d | %s | %s | %.2f |", r.name, r.Commits, r.Errors, ms(r.WP99), ms(r.WMax), r.ReadOps, ms(r.RP99), ms(r.RMax), r.OpenReads)
		for _, at := range []int{10, 20} {
			if at <= len(r.Series) {
				fmt.Fprintf(&b, " %s |", mb(r.Series[at-1]))
			} else {
				b.WriteString(" |")
			}
		}
		timed := ""
		if r.Checkpoints > 0 {
			timed = fmt.Sprintf("%d, %d busy, slowest %s ms", r.Checkpoints, r.Busy, ms(r.CheckpointMax))
		}
		fmt.Fprintf(&b, " %s | %s | %d | %s | %s |\n", mb(r.Max), mb(r.End), r.Restarts, mb(r.medianLag()), timed)
	}
	return b.String()
}

// writeMatrixResults writes results/<date>-wal-matrix.tsv and puts the
// wal-matrix section into results/latest.md, replacing an older one and
// leaving the rest of the file alone.
func writeMatrixResults(started time.Time) error {
	err := os.MkdirAll("results", 0o755)
	if err != nil {
		return err
	}
	var tsv strings.Builder
	tsv.WriteString("group\tcell\twriter_conns\treaders\tseconds\tcommits\terrors\tlost\twrite_p99_ms\twrite_max_ms\treads\tread_p99_ms\tread_max_ms\topen_reads\twal_max_mb\twal_end_mb\trestarts\tnot_checkpointed_median_mb\ttimed_checkpoints\ttimed_busy\ttimed_slowest_ms\twal_mb_per_second\trestarts_per_second\tnot_checkpointed_mb_per_second\tfirst_error\n")
	for _, r := range matrixResults {
		var wal, restarts, lag []string
		for i := range r.Series {
			wal = append(wal, mb(r.Series[i]))
			restarts = append(restarts, fmt.Sprint(r.RestartSeries[i]))
			lag = append(lag, mb(r.LagSeries[i]))
		}
		fmt.Fprintf(&tsv, "%s\t%s\t%d\t%s\t%.1f\t%d\t%d\t%d\t%s\t%s\t%d\t%s\t%s\t%.2f\t%s\t%s\t%d\t%s\t%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			r.group, r.name, r.conns, r.readers, r.Elapsed.Seconds(), r.Commits, r.Errors, r.Lost, ms(r.WP99), ms(r.WMax),
			r.ReadOps, ms(r.RP99), ms(r.RMax), r.OpenReads, mb(r.Max), mb(r.End), r.Restarts, mb(r.medianLag()),
			r.Checkpoints, r.Busy, ms(r.CheckpointMax), strings.Join(wal, ","), strings.Join(restarts, ","), strings.Join(lag, ","),
			strings.TrimSpace(r.FirstError+" "+r.CheckpointError))
	}
	err = os.WriteFile(filepath.Join("results", started.Format("2006-01-02")+"-wal-matrix.tsv"), []byte(tsv.String()), 0o644)
	if err != nil {
		return err
	}

	var md strings.Builder
	fmt.Fprintf(&md, "Run of %s, %s, GOMAXPROCS %d, %s. %d writer goroutines offered %d commits/s of %d-byte rows for %s per cell; %d readers on their own read-only pool of %d, every read one `Store.Read` transaction. Timed checkpoints run every %s; `Store.Checkpoint` gets a quiet window of %s unless the cell names another. The claim cells run their build's own load instead: see `matrixCells`. Sizes in MB, latencies in ms.\n\n",
		started.Format(time.RFC3339), runtime.Version(), runtime.GOMAXPROCS(0), engine(), matrixWriters, matrixRate, matrixPayload, matrixDuration, matrixReaders, matrixReaders, matrixCheckpointEvery, matrixQuiet)
	md.WriteString("`restarts` counts how often the WAL started over from its first frame: the increase of salt-1 in the wal-index header, which SQLite bumps by one per restart. `not checkpointed` is the median, over the per-second samples, of the WAL that no checkpoint had copied into the database yet: near zero beside a large WAL means checkpoints finish and the WAL still cannot restart, near the WAL size means they cannot finish. `open reads` is the mean number of reads in flight.\n\n")
	for _, g := range []struct{ group, title string }{
		{"matrix", "Writer connections x reader pattern"},
		{"mitigation", "Mitigations"},
		{"claim", "The original claims"},
	} {
		if table := matrixTable(matrixResults, g.group); strings.Count(table, "\n") > 2 {
			fmt.Fprintf(&md, "### %s\n\n%s\n", g.title, table)
		}
	}

	const heading = "\n## wal-matrix\n\n"
	latest := filepath.Join("results", "latest.md")
	old, _ := os.ReadFile(latest)
	head, rest, _ := strings.Cut(string(old), heading)
	tail := ""
	if i := strings.Index(rest, "\n## "); i >= 0 {
		tail = rest[i:]
	}
	return os.WriteFile(latest, []byte(strings.TrimRight(head, "\n")+"\n"+heading+strings.TrimRight(md.String(), "\n")+"\n"+tail), 0o644)
}
