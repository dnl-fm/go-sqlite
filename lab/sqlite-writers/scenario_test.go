package writers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/sqlite"
)

// Scenario shape, fixed by the spec: 16 writers, 8 readers, a 100ms long
// transaction, 4 processes for the multi-process strategy.
//
// Readers pause 1ms between iterations so every strategy faces the same
// offered read load. A reader pool that never pauses is its own experiment
// (see the wal-growth scenario); left in here it would make the strategies
// do wildly different amounts of read work and the write numbers
// incomparable.
const (
	scenarioWriters   = 16
	scenarioReaders   = 8
	scenarioReadPause = time.Millisecond
	longTxInserters   = 8
	longTxHold        = 100 * time.Millisecond
	scenarioProcesses = 4
	holderWorker      = 99
	childEnv          = "LAB_WRITERS_CHILD_JOB"
)

func fullMode() bool { return os.Getenv("LAB_FULL") != "" }

// sizes returns ops per writer for the counted workloads and the duration of
// the long-tx workload.
func sizes() (inserts, counter int, longTx time.Duration) {
	if fullMode() {
		return 10000, 5000, 10 * time.Second
	}
	return 200, 100, 1500 * time.Millisecond
}

// target is one strategy opened against one database file.
type target struct {
	write func(context.Context, func(*sql.Tx) error) error
	rdb   *sql.DB // where reads go
	wdb   *sql.DB // where maintenance (checkpoints) goes
	close func()
}

// openNaive is the anti-pattern: one pool of 4 for everything. Strategy A
// leaves BEGIN deferred, strategy B makes it BEGIN IMMEDIATE.
func openNaive(path string, immediate bool, extraPragmas ...string) (*target, error) {
	d := dsn(path, "NORMAL", false)
	if !immediate {
		d = strings.Replace(d, "&_txlock=immediate", "", 1)
	}
	for _, pragma := range extraPragmas {
		d += "&_pragma=" + pragma
	}
	db, err := sql.Open("sqlite", d)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	write := func(ctx context.Context, fn func(*sql.Tx) error) error {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		err = fn(tx)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	return &target{write: write, rdb: db, wdb: db, close: func() { _ = db.Close() }}, db.Ping()
}

func openSplit(path, synchronous string, queue bool) (*target, error) {
	store, err := Open(context.Background(), path, Options{Synchronous: synchronous})
	if err != nil {
		return nil, err
	}
	if !queue {
		return &target{write: store.Write, rdb: store.Reader, wdb: store.Writer, close: func() { _ = store.Close() }}, nil
	}
	q := NewQueue(store, QueueOptions{})
	return &target{write: q.Submit, rdb: store.Reader, wdb: store.Writer, close: func() { _ = q.Close(); _ = store.Close() }}, nil
}

type strategy struct {
	name        string
	strict      bool   // C, D, E: must be correct. A, B: failures are data.
	processes   int    // > 1 re-executes the test binary
	synchronous string // for the children of a multi-process strategy
	open        func(path string) (*target, error)
}

func strategies() []strategy {
	return []strategy{
		{name: "A naive", open: func(p string) (*target, error) { return openNaive(p, false) }},
		{name: "B immediate", open: func(p string) (*target, error) { return openNaive(p, true) }},
		{name: "C split", strict: true, open: func(p string) (*target, error) { return openSplit(p, "NORMAL", false) }},
		{name: "D queue", strict: true, open: func(p string) (*target, error) { return openSplit(p, "NORMAL", true) }},
		{name: "E multi-process", strict: true, processes: scenarioProcesses, synchronous: "NORMAL"},
	}
}

// fullSyncStrategies are C and D again with synchronous=FULL, where every
// commit is an fsync. They exist to measure the one thing the queue is for.
func fullSyncStrategies() []strategy {
	return []strategy{
		{name: "C split (FULL)", strict: true, open: func(p string) (*target, error) { return openSplit(p, "FULL", false) }},
		{name: "D queue (FULL)", strict: true, open: func(p string) (*target, error) { return openSplit(p, "FULL", true) }},
	}
}

// plan is the share of a workload one process runs.
type plan struct {
	Workload string
	Process  int
	Writers  int
	Readers  int
	N        int           // ops per writer; 0 means run for Duration
	Duration time.Duration // long-tx only
	Holder   bool          // long-tx only: also run the 100ms transaction loop

	ReadPause  time.Duration // between reader iterations
	WritePause time.Duration // between a writer's ops (wal-growth only)
}

// sample is what one process measured. It crosses the process boundary as gob.
type sample struct {
	Ops        int // acknowledged writes
	Busy       int
	Other      int
	FirstBusy  string
	FirstOther string
	WriteNS    []int64
	ReadNS     []int64
	Elapsed    time.Duration
	WALBytes   int64 // size of the -wal file when the writers finished
}

func (s *sample) fail(what string, err error) {
	var se *sqlite.Error
	busy := strings.Contains(err.Error(), "database is locked")
	if errors.As(err, &se) && (se.Code()&0xff == 5 || se.Code()&0xff == 6) { // SQLITE_BUSY, SQLITE_LOCKED
		busy = true
	}
	if busy {
		s.Busy++
		if s.FirstBusy == "" {
			s.FirstBusy = what + ": " + err.Error()
		}
		return
	}
	s.Other++
	if s.FirstOther == "" {
		s.FirstOther = what + ": " + err.Error()
	}
}

func (s *sample) merge(o sample) {
	s.Ops += o.Ops
	s.Busy += o.Busy
	s.Other += o.Other
	if s.FirstBusy == "" {
		s.FirstBusy = o.FirstBusy
	}
	if s.FirstOther == "" {
		s.FirstOther = o.FirstOther
	}
	s.WriteNS = append(s.WriteNS, o.WriteNS...)
	s.ReadNS = append(s.ReadNS, o.ReadNS...)
	s.Elapsed = max(s.Elapsed, o.Elapsed)
	s.WALBytes = max(s.WALBytes, o.WALBytes)
}

func walSize(dbPath string) int64 {
	fi, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0
	}
	return fi.Size()
}

const payload = "the quick brown fox jumps over the lazy dog, again and again and again"

func insertOp(worker, seq int) func(*sql.Tx) error {
	return func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO items (worker, seq, payload) VALUES (?, ?, ?)", worker, seq, payload)
		return err
	}
}

// counterOp is the classic upgrade: read, then write what was read plus one.
func counterOp(tx *sql.Tx) error {
	var v int64
	err := tx.QueryRow("SELECT v FROM counter WHERE id = 1").Scan(&v)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE counter SET v = ? WHERE id = 1", v+1)
	return err
}

// runPlan runs one process's share of a workload: writers until done, readers
// alongside them the whole time.
func runPlan(p plan, tg *target, dbPath string) sample {
	ctx := context.Background()
	var (
		total            sample
		mu               sync.Mutex
		writers, readers sync.WaitGroup
	)
	stop := make(chan struct{})
	began := time.Now()
	deadline := began.Add(p.Duration)
	more := func(seq int) bool {
		if p.N > 0 {
			return seq < p.N
		}
		return time.Now().Before(deadline)
	}
	finish := func(local sample) {
		mu.Lock()
		total.merge(local)
		mu.Unlock()
	}

	for range p.Readers {
		readers.Go(func() {
			var local sample
			defer func() { finish(local) }()
			for {
				select {
				case <-stop:
					return
				default:
				}
				var n, v int64
				t0 := time.Now()
				err := tg.rdb.QueryRowContext(ctx, "SELECT count(*) FROM items").Scan(&n)
				if err == nil {
					err = tg.rdb.QueryRowContext(ctx, "SELECT v FROM counter WHERE id = 1").Scan(&v)
				}
				if err != nil {
					local.fail("read", err)
					continue
				}
				local.ReadNS = append(local.ReadNS, int64(time.Since(t0)))
				if p.ReadPause > 0 {
					time.Sleep(p.ReadPause)
				}
			}
		})
	}
	for w := range p.Writers {
		writers.Go(func() {
			var local sample
			defer func() { finish(local) }()
			worker := p.Process*100 + w
			for seq := 0; more(seq); seq++ {
				op := counterOp
				if p.Workload != "counter" {
					op = insertOp(worker, seq)
				}
				t0 := time.Now()
				err := tg.write(ctx, op)
				if err != nil {
					local.fail("write", err)
					continue
				}
				local.Ops++
				local.WriteNS = append(local.WriteNS, int64(time.Since(t0)))
				if p.WritePause > 0 {
					time.Sleep(p.WritePause)
				}
			}
		})
	}
	if p.Holder {
		writers.Go(func() {
			var local sample
			defer func() { finish(local) }()
			for seq := 0; more(seq); seq++ {
				insert := insertOp(p.Process*100+holderWorker, seq)
				err := tg.write(ctx, func(tx *sql.Tx) error {
					if err := insert(tx); err != nil {
						return err
					}
					time.Sleep(longTxHold) // the write lock is held the whole time
					return nil
				})
				if err != nil {
					local.fail("long tx", err)
					continue
				}
				local.Ops++ // a row, but not a queued-writer latency sample
			}
		})
	}
	writers.Wait()
	elapsed := time.Since(began)
	wal := walSize(dbPath)
	close(stop)
	readers.Wait()
	total.Elapsed, total.WALBytes = elapsed, wal
	return total
}

// plans splits a workload over the strategy's processes. n is ops per writer
// for the counted workloads.
func plans(workload string, n, processes int) []plan {
	_, _, longTx := sizes()
	out := make([]plan, processes)
	for i := range out {
		p := plan{
			Workload: workload, Process: i, N: n,
			Writers: scenarioWriters / processes, Readers: scenarioReaders / processes, ReadPause: scenarioReadPause,
		}
		if workload == "long-tx" {
			p.N, p.Writers, p.Duration, p.Holder = 0, longTxInserters/processes, longTx, i == 0
		}
		out[i] = p
	}
	return out
}

// row is one line of the results table.
type row struct {
	Workload, Strategy     string
	Ops, Busy, Other, Lost int
	Elapsed                time.Duration
	WP50, WP99, WMax       time.Duration
	RP50, RP99             time.Duration
	WALBytes               int64
	FirstBusy, FirstOther  string
}

func (r row) opsPerSec() float64 { return float64(r.Ops) / r.Elapsed.Seconds() }

func percentile(sorted []int64, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	return time.Duration(sorted[min(len(sorted)-1, int(p*float64(len(sorted))))])
}

// runScenario runs one strategy on one workload (n ops per writer) against a
// fresh database and checks what actually landed in the file.
func runScenario(t *testing.T, st strategy, workload string, n int) row {
	t.Helper()
	dir := labDir(t)
	dbPath := filepath.Join(dir, "lab.db")
	setup, err := Open(context.Background(), dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = setup.Writer.Exec(schema)
	if err != nil {
		t.Fatal(err)
	}
	err = setup.Close()
	if err != nil {
		t.Fatal(err)
	}

	var s sample
	if st.processes > 1 {
		s = runProcesses(t, dir, dbPath, st.synchronous, plans(workload, n, st.processes))
	} else {
		tg, err := st.open(dbPath)
		if err != nil {
			t.Fatal(err)
		}
		s = runPlan(plans(workload, n, 1)[0], tg, dbPath)
		tg.close()
	}

	check, err := Open(context.Background(), dbPath, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	landed := countItems(t, check.Reader)
	if workload == "counter" {
		err = check.Reader.QueryRow("SELECT v FROM counter WHERE id = 1").Scan(&landed)
		if err != nil {
			t.Fatal(err)
		}
	}

	slices.Sort(s.WriteNS)
	slices.Sort(s.ReadNS)
	r := row{
		Workload: workload, Strategy: st.name,
		Ops: s.Ops, Busy: s.Busy, Other: s.Other, Lost: s.Ops - landed, Elapsed: s.Elapsed,
		WP50: percentile(s.WriteNS, 0.50), WP99: percentile(s.WriteNS, 0.99), WMax: percentile(s.WriteNS, 1),
		RP50: percentile(s.ReadNS, 0.50), RP99: percentile(s.ReadNS, 0.99), WALBytes: s.WALBytes,
		FirstBusy: s.FirstBusy, FirstOther: s.FirstOther,
	}

	if st.strict {
		// Separate processes wait in SQLite's busy handler, which is a polling
		// retry, not a queue: behind back-to-back long transactions a writer can
		// lose every race until busy_timeout expires. That SQLITE_BUSY is the
		// finding for E/long-tx, not a defect; every other error still fails.
		busyAllowed := workload == "long-tx" && st.processes > 1
		if r.Other != 0 || (r.Busy != 0 && !busyAllowed) {
			t.Errorf("%s/%s: %d busy + %d other errors, want none (%s %s)", workload, st.name, r.Busy, r.Other, r.FirstBusy, r.FirstOther)
		}
		if r.Lost != 0 {
			t.Errorf("%s/%s: %d acknowledged writes, %d in the database", workload, st.name, r.Ops, landed)
		}
		if want := scenarioWriters * n; workload != "long-tx" && landed != want {
			t.Errorf("%s/%s: %d in the database, want exactly %d", workload, st.name, landed, want)
		}
		if workload == "long-tx" && st.processes <= 1 && r.RP99 >= 50*time.Millisecond {
			t.Errorf("%s/%s: reader p99 %v while a writer held the lock for %v, want < 50ms", workload, st.name, r.RP99, longTxHold)
		}
	}
	return r
}

// results collects every row of this test run; TestMain writes it to disk in
// full mode, but only when every one of the scenarioCells cells ran.
var (
	results       []row
	scenarioCells int
)

func TestScenario(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		t.Skip("child process")
	}
	inserts, counter, _ := sizes()
	type cell struct {
		workload string
		n        int
		st       strategy
	}
	var cells []cell
	for _, st := range strategies() {
		cells = append(cells, cell{"inserts", inserts, st})
	}
	for _, st := range fullSyncStrategies() {
		cells = append(cells, cell{"inserts", inserts / 20, st})
	}
	for _, st := range strategies() {
		cells = append(cells, cell{"counter", counter, st})
	}
	for _, st := range strategies() {
		cells = append(cells, cell{"long-tx", 0, st})
	}
	scenarioCells = len(cells)
	for _, c := range cells {
		t.Run(c.workload+"/"+c.st.name, func(t *testing.T) {
			r := runScenario(t, c.st, c.workload, c.n)
			results = append(results, r)
			if r.FirstBusy != "" {
				t.Logf("%d busy errors, first: %s", r.Busy, r.FirstBusy)
			}
			if r.FirstOther != "" {
				t.Logf("%d other errors, first: %s", r.Other, r.FirstOther)
			}
		})
	}
	t.Log("\n" + table(results))
}

func ms(d time.Duration) string { return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond)) }

func mb(bytes int64) string { return fmt.Sprintf("%.1f", float64(bytes)/(1<<20)) }

// table renders rows as a markdown table. Latencies are milliseconds; "lost"
// is acknowledged writes missing from the database; "wal MB" is the -wal file
// when the writers finished.
func table(rows []row) string {
	var b strings.Builder
	b.WriteString("| workload | strategy | ops | busy | other | lost | ops/s | write p50 | write p99 | write max | read p50 | read p99 | wal MB |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %d | %.0f | %s | %s | %s | %s | %s | %s |\n",
			r.Workload, r.Strategy, r.Ops, r.Busy, r.Other, r.Lost, r.opsPerSec(),
			ms(r.WP50), ms(r.WP99), ms(r.WMax), ms(r.RP50), ms(r.RP99), mb(r.WALBytes))
	}
	return b.String()
}
