package writers

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const loadCells = 24 // four patterns, including both burst deadline contracts
const loadTick = 100 * time.Microsecond

// Fixed-size histograms avoid making a growing measurement slice look like
// an unbounded writer queue. Percentiles round UP to the next 100us.
type loadHistogram struct {
	bins [10001]uint32
	n    int
	max  time.Duration
}

func (h *loadHistogram) add(d time.Duration) {
	h.bins[min(int((d+loadTick-1)/loadTick), len(h.bins)-1)]++
	h.n++
	h.max = max(h.max, d)
}
func (h *loadHistogram) p99() time.Duration {
	if h.n == 0 {
		return 0
	}
	want := int(math.Ceil(float64(h.n) * .99))
	n := 0
	for i, v := range h.bins {
		n += int(v)
		if n >= want {
			if i == len(h.bins)-1 {
				return h.max
			}
			return time.Duration(i) * loadTick
		}
	}
	return h.max
}

type loadMemory struct {
	At                  time.Duration
	Heap                uint64
	Goroutines, Pending int
}
type loadSync struct{ P50, P99, Max time.Duration }

func (s loadSync) bound() time.Duration { return 3 * s.P99 }

// Time the sync syscall itself, without histogram rounding or write time.
func measureLoadSync(t *testing.T) loadSync {
	t.Helper()
	f, err := os.CreateTemp(loadBase(t), "sync-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	block := make([]byte, 4096)
	latencies := make([]time.Duration, 200)
	for i := range latencies {
		if n, err := f.Write(block); err != nil || n != len(block) {
			t.Fatalf("disk probe write: bytes=%d error=%v", n, err)
		}
		start := time.Now()
		err := syscall.Fdatasync(int(f.Fd()))
		latencies[i] = time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(latencies)
	return loadSync{latencies[99], latencies[197], latencies[199]}
}

func loadBase(t *testing.T) string {
	t.Helper()
	// Deliberately ignore LAB_DIR: durability probes must use the same real disk.
	base, err := filepath.Abs(".labdata")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	var fs syscall.Statfs_t
	if err = syscall.Statfs(base, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Type != 0xef53 {
		t.Fatalf("load requires ext4 .labdata, filesystem type %#x", fs.Type)
	}
	return base
}

type loadRow struct {
	Sync                                                                         loadSync
	Full                                                                         bool
	Pattern, Phase                                                               string
	Capacity, CapacityTx, Rate                                                   float64
	Duration, Elapsed, Deadline, Admission                                       time.Duration
	Scheduled, Accepted, Rejected, Errors, ReadErrors, Reads, Transactions, Lost int
	WP99, WMax, RejectMax, RP99, Gap, Drain                                      time.Duration
	FirstError                                                                   string
	Samples                                                                      []loadMemory
	Plateau                                                                      bool
}

var loadResults []loadRow

// Repeated filtered runs can have 24 rows without covering all 24 cells.
func completeLoadResults(rows []loadRow) bool {
	seen := make(map[string]bool, loadCells)
	for _, r := range rows {
		seen[r.Pattern+"/"+r.Phase] = true
	}
	for _, pattern := range []string{"C-NORMAL", "D-NORMAL", "C-FULL", "D-FULL"} {
		for _, phase := range []string{"calibration", "50%", "80%", "burst", "burst-short-deadline", "overload"} {
			if !seen[pattern+"/"+phase] {
				return false
			}
		}
	}
	return true
}

// Only the last tx is held. A transaction's observation is released with its
// last caller, rather than retaining every transaction during the memory probe.
type loadTransaction struct{ acknowledged bool }
type loadWindow struct {
	writes               loadHistogram
	scheduled, completed int
	observed             time.Duration
}
type loadMetrics struct {
	mu                                                             sync.Mutex
	writes, rejects, reads                                         loadHistogram
	expected                                                       [64]int
	inserts, scheduled, accepted, rejected, failures, readFailures int
	commits, intervalAccepted, intervalCommits                     int
	firstError                                                     string
	lastTx                                                         *sql.Tx
	transaction                                                    *loadTransaction
	lastCommit                                                     time.Time
	gap                                                            time.Duration
	windows                                                        []loadWindow
	start, end                                                     time.Time
	pending                                                        atomic.Int64
}

func TestScenarioLoad(t *testing.T) {
	if os.Getenv(childEnv) != "" {
		t.Skip("child process")
	}
	disk := measureLoadSync(t)
	t.Logf("disk fdatasync (200 x 4KiB): p50=%s p99=%s max=%s sustained bound=%s", disk.P50, disk.P99, disk.Max, disk.bound())
	calibration, sustained, burst, spike, overload := 500*time.Millisecond, 1500*time.Millisecond, time.Second, 200*time.Millisecond, 1500*time.Millisecond
	if fullMode() {
		// At 200%, a 2s spike above 50% background adds about 3s of
		// capacity's work. With 50% spare, drain takes about 6s. Start
		// the spike at 1s and leave 10s after it, including observation.
		calibration, sustained, burst, spike, overload = 10*time.Second, 60*time.Second, 13*time.Second, 2*time.Second, 20*time.Second
	}
	for _, pattern := range []string{"C-NORMAL", "D-NORMAL", "C-FULL", "D-FULL"} {
		t.Run(pattern, func(t *testing.T) {
			var capacity, txCapacity float64
			// Calibration still runs when a single dependent phase is selected. It
			// only counts as a completed cell if its named subtest was selected.
			calibrated := false
			calibrate := func(ct *testing.T) {
				r := runLoad(ct, pattern, "calibration", calibration, 0, 0, 0, 0, disk)
				capacity, txCapacity = r.Capacity, r.CapacityTx
				calibrated = true
				if capacity <= 0 {
					ct.Fatal("no calibrated capacity")
				}
				loadResults = append(loadResults, r)
			}
			t.Run("calibration", calibrate)
			for _, phase := range []string{"50%", "80%", "burst", "burst-short-deadline", "overload"} {
				t.Run(phase, func(t *testing.T) {
					if !calibrated {
						r := runLoad(t, pattern, "calibration", calibration, 0, 0, 0, 0, disk)
						capacity, txCapacity = r.Capacity, r.CapacityTx
						calibrated = true
					}
					if capacity <= 0 {
						t.Fatal("no calibrated capacity")
					}
					duration, rate, deadline := sustained, capacity*.5, time.Second
					var spikeDuration time.Duration
					switch phase {
					case "80%":
						rate = capacity * .8
					case "burst":
						duration, spikeDuration, deadline = burst, spike, 5*time.Second
					case "burst-short-deadline":
						duration, spikeDuration = burst, spike
					case "overload":
						duration, rate, deadline = overload, capacity*1.5, 100*time.Millisecond
					}
					r := runLoad(t, pattern, phase, duration, rate, spikeDuration, deadline, capacity, disk)
					r.Capacity, r.CapacityTx = capacity, txCapacity
					loadResults = append(loadResults, r)
				})
			}
		})
	}
	t.Log("\n" + loadTable(loadResults))
}

func runLoad(t *testing.T, pattern, phase string, duration time.Duration, rate float64, spike, deadline time.Duration, capacity float64, disk loadSync) loadRow {
	t.Helper()
	requestBudget := max(time.Second, deadline)
	if phase == "calibration" {
		// Calibration measures all 64 submitters, including unlucky waiters
		// in C's unordered pool. A 1s limit here would bias its capacity.
		requestBudget = duration + 10*time.Second
		deadline = requestBudget
	}
	dir, err := os.MkdirTemp(loadBase(t), "load-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	store, err := Open(context.Background(), filepath.Join(dir, "load.db"), Options{Readers: 8, Synchronous: strings.Split(pattern, "-")[1]})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	err = store.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.Exec("CREATE TABLE load_items(id INTEGER PRIMARY KEY, payload TEXT NOT NULL); CREATE TABLE load_hot(id INTEGER PRIMARY KEY,v INTEGER NOT NULL)"); err != nil {
			return err
		}
		for id := range 64 {
			if _, err := tx.Exec("INSERT INTO load_hot VALUES (?,0)", id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	write := store.Write
	if strings.HasPrefix(pattern, "D") {
		q := NewQueue(store, QueueOptions{})
		defer q.Close()
		write = q.Submit
	}
	windowDuration := 250 * time.Millisecond
	if spike > 0 && !fullMode() {
		windowDuration = 100 * time.Millisecond
	}
	m := &loadMetrics{windows: make([]loadWindow, int(duration/windowDuration)+1)}
	var readers sync.WaitGroup
	readCtx, stopReaders := context.WithCancel(context.Background())
	for id := range 8 {
		readers.Add(1)
		go func(id int) {
			defer readers.Done()
			for readCtx.Err() == nil {
				began := time.Now()
				err := store.Read(context.Background(), func(tx *sql.Tx) error {
					var v int
					return tx.QueryRow("SELECT v FROM load_hot WHERE id=?", id).Scan(&v)
				})
				m.mu.Lock()
				m.reads.add(time.Since(began))
				if err != nil {
					m.readFailures++
					m.fail(err)
				}
				m.mu.Unlock()
				timer := time.NewTimer(time.Millisecond)
				select {
				case <-readCtx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}
		}(id)
	}
	// Allocate the bounded collectors and warm the reader pool before sampling.
	runtime.GC()
	m.start = time.Now()
	m.end = m.start.Add(duration)
	m.lastCommit = m.start
	samples := make([]loadMemory, 0, int(duration/(200*time.Millisecond))+2)
	sampleDone := make(chan struct{})
	stopSamples := make(chan struct{})
	go func() {
		defer close(sampleDone)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopSamples:
				return
			case at := <-ticker.C:
				if at.After(m.end) {
					continue
				}
				var mem runtime.MemStats
				runtime.ReadMemStats(&mem)
				samples = append(samples, loadMemory{at.Sub(m.start), mem.HeapAlloc, runtime.NumGoroutine(), int(m.pending.Load())})
			}
		}
	}()
	var requests sync.WaitGroup
	var seq atomic.Int64
	submit := func(id int64, scheduled time.Time, budget time.Duration) {
		defer requests.Done()
		defer m.pending.Add(-1)
		requestCtx, cancelRequest := context.WithDeadline(context.Background(), scheduled.Add(requestBudget))
		defer cancelRequest()
		ctx, cancel := context.WithDeadline(requestCtx, scheduled.Add(budget))
		defer cancel()
		var observation *loadTransaction
		fn := func(tx *sql.Tx) error {
			m.mu.Lock()
			if tx != m.lastTx {
				m.lastTx = tx
				m.transaction = &loadTransaction{}
			}
			observation = m.transaction
			m.mu.Unlock()
			if id%10 != 0 {
				_, err := tx.ExecContext(context.WithoutCancel(ctx), "INSERT INTO load_items(id,payload) VALUES (?,?)", id, strings.Repeat("x", 512))
				return err
			}
			hot := (id / 10) % 64
			var v int
			if err := tx.QueryRow("SELECT v FROM load_hot WHERE id=?", hot).Scan(&v); err != nil {
				return err
			}
			_, err := tx.Exec("UPDATE load_hot SET v=? WHERE id=?", v+1, hot)
			return err
		}
		err := write(ctx, fn)
		ended := time.Now()
		latency := ended.Sub(scheduled)
		m.mu.Lock()
		defer m.mu.Unlock()
		window := int(scheduled.Sub(m.start) / windowDuration)
		if window >= 0 && window < len(m.windows) {
			m.windows[window].completed++
			m.windows[window].observed = max(m.windows[window].observed, ended.Sub(m.start))
		}
		if err == nil {
			m.accepted++
			m.writes.add(latency)
			if id%10 == 0 {
				m.expected[(id/10)%64]++
			} else {
				m.inserts++
			}
			if !ended.After(m.end) {
				m.intervalAccepted++
			}
			if observation != nil && !observation.acknowledged {
				observation.acknowledged = true
				m.commits++
				if !ended.After(m.end) {
					m.intervalCommits++
				}
				if !m.lastCommit.IsZero() {
					m.gap = max(m.gap, time.Since(m.lastCommit))
				}
				m.lastCommit = time.Now()
			}
			if window >= 0 && window < len(m.windows) {
				m.windows[window].writes.add(latency)
			}
		} else if errors.Is(err, context.DeadlineExceeded) {
			m.rejected++
			m.rejects.add(latency)
		} else {
			m.failures++
			m.fail(err)
		}
	}
	launch := func(scheduled time.Time, budget time.Duration) {
		requests.Add(1)
		m.pending.Add(1)
		m.mu.Lock()
		m.scheduled++
		window := int(scheduled.Sub(m.start) / windowDuration)
		if window >= 0 && window < len(m.windows) {
			m.windows[window].scheduled++
		}
		m.mu.Unlock()
		go submit(seq.Add(1), scheduled, budget)
	}
	if phase == "calibration" {
		for range 64 {
			requests.Add(1)
			go func() {
				defer requests.Done()
				for time.Now().Before(m.end) {
					requests.Add(1)
					m.pending.Add(1)
					m.mu.Lock()
					m.scheduled++
					m.mu.Unlock()
					submit(seq.Add(1), time.Now(), deadline)
				}
			}()
		}
	} else {
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		// Fix the spike early; extending recovery must not move its end.
		spikeStart := spike / 2
		spikeEnd := spikeStart + spike
		for elapsed := time.Duration(0); elapsed < duration; {
			scheduled := m.start.Add(elapsed)
			if wait := time.Until(scheduled); wait > 0 {
				timer.Reset(wait)
				<-timer.C
			}
			launch(scheduled, deadline)
			offered := rate
			if spike > 0 && elapsed >= spikeStart && elapsed < spikeEnd {
				offered = capacity * 2
			}
			elapsed += time.Duration(float64(time.Second) / offered)
		}
		timer.Stop()
	}
	requests.Wait()
	if wait := time.Until(m.end); wait > 0 {
		time.Sleep(wait)
	}
	close(stopSamples)
	<-sampleDone
	stopReaders()
	readers.Wait()
	elapsed := time.Since(m.start)
	if m.lastCommit.Before(m.end) {
		m.gap = max(m.gap, m.end.Sub(m.lastCommit))
	}
	lost := 0
	var rows int
	if err = store.Reader.QueryRow("SELECT count(*) FROM load_items").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	lost += absLoad(rows - m.inserts)
	hotRows, err := store.Reader.Query("SELECT id,v FROM load_hot ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for hotRows.Next() {
		var id, v int
		if err = hotRows.Scan(&id, &v); err != nil {
			t.Fatal(err)
		}
		lost += absLoad(v - m.expected[id])
		n++
	}
	err = hotRows.Err()
	_ = hotRows.Close()
	if err != nil {
		t.Fatal(err)
	}
	if n != 64 {
		t.Errorf("hot row count %d, want 64", n)
	}
	r := loadRow{Sync: disk, Full: fullMode(), Pattern: pattern, Phase: phase, Rate: rate, Duration: duration, Elapsed: elapsed, Deadline: requestBudget, Admission: deadline, Capacity: capacity, Scheduled: m.scheduled, Accepted: m.accepted, Rejected: m.rejected, Errors: m.failures, ReadErrors: m.readFailures, Reads: m.reads.n, Transactions: m.commits, Lost: lost, WP99: m.writes.p99(), WMax: m.writes.max, RejectMax: m.rejects.max, RP99: m.reads.p99(), Gap: m.gap, FirstError: m.firstError, Samples: samples, Plateau: loadPlateau(samples, duration), Drain: -1}
	if phase == "calibration" {
		r.Capacity = float64(m.intervalAccepted) / duration.Seconds()
		r.CapacityTx = float64(m.intervalCommits) / duration.Seconds()
	}
	if spike > 0 {
		r.Drain = loadDrain(m.windows, duration, spike, windowDuration, disk.bound())
	}
	loadAssert(t, r)
	t.Logf("%s/%s: requests %d accepted %d rejected %d errors %d lost %d, capacity %.0f req/s %.0f tx/s, write p99/max %s/%s ms read p99 %s ms commit gap %s ms, drain %s plateau %t first error %q; %s", pattern, phase, r.Scheduled, r.Accepted, r.Rejected, r.Errors, r.Lost, r.Capacity, r.CapacityTx, ms(r.WP99), ms(r.WMax), ms(r.RP99), ms(r.Gap), r.Drain, r.Plateau, r.FirstError, loadVerdict(r))
	return r
}
