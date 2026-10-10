package tenants

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var bg = context.Background()

func TestScenario01_IDValidation(t *testing.T) {
	base := labRoot(t)
	m := newManager(t, filepath.Join(base, "root"), 0, schemaV1)

	invalid := []string{
		"", ".", "..", "../x", "../../etc/passwd", "/etc/passwd", "a/b", `a\b`, "a/../b",
		"A", "Acme", "acmE", "a.b", "a.db", ".hidden", "a b", " a", "a\n", "a\x00", "é", "a:b", "a?mode=memory",
		"-a", "_a", strings.Repeat("a", 64),
	}
	for _, id := range invalid {
		for op, err := range map[string]error{
			"Create": m.Create(bg, id),
			"With":   m.With(bg, id, func(*Tenant) error { return nil }),
			"Delete": m.Delete(bg, id),
			"Backup": m.Backup(bg, id, filepath.Join(base, "backup.db")),
		} {
			if !errors.Is(err, ErrInvalidID) {
				t.Errorf("%s(%q) = %v, want ErrInvalidID", op, id, err)
			}
		}
	}
	// Rejected before touching the filesystem: nothing inside root, nothing next to it.
	if entries, _ := os.ReadDir(filepath.Join(base, "root")); len(entries) != 0 {
		t.Errorf("root not empty after invalid IDs: %v", entries)
	}
	if entries, _ := os.ReadDir(base); len(entries) != 1 {
		t.Errorf("files escaped root: %v", entries)
	}

	valid := []string{"a", "0", "acme", "acme-prod_2", "0-_", strings.Repeat("a", 63)}
	mustCreate(t, m, valid...)
	ids, err := m.IDs()
	if err != nil || len(ids) != len(valid) {
		t.Fatalf("IDs() = %v, %v; want %d tenants", ids, err, len(valid))
	}
}

func TestScenario02_Lifecycle(t *testing.T) {
	root := labRoot(t)
	m := newManager(t, root, 0, schemaV1)
	db := filepath.Join(root, "acme.db")

	// Unknown tenant: ErrNotFound, and no file appears. A typo must not create a tenant.
	if err := insert(bg, m, "acme", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("With(unknown) = %v, want ErrNotFound", err)
	}
	if err := m.Delete(bg, "acme"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Delete(unknown) = %v, want ErrNotFound", err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Fatalf("unknown tenant left files behind: %v", entries)
	}

	mustCreate(t, m, "acme")
	if err := m.Create(bg, "acme"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create = %v, want ErrExists", err)
	}
	if err := insert(bg, m, "acme", 0, 0); err != nil {
		t.Fatal(err)
	}
	if n := mustQueryInt(t, m, "acme", countSQL); n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
	// The reader pool is query_only: a write in Read fails instead of bypassing the writer queue.
	err := m.With(bg, "acme", func(tn *Tenant) error {
		return tn.Read(bg, func(tx *sql.Tx) error {
			_, err := tx.Exec(insertSQL, 0, 1)
			return err
		})
	})
	if err == nil {
		t.Fatal("write inside Read succeeded")
	}
	// Create also refuses a tenant that exists on disk but is not open.
	m2 := newManager(t, root, 0, schemaV1)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m2.Create(bg, "acme"); !errors.Is(err, ErrExists) {
		t.Fatalf("Create over closed tenant = %v, want ErrExists", err)
	}
	m = m2

	// Delete while a goroutine is inside With.
	inside, leave, userDone := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		userDone <- m.With(bg, "acme", func(tn *Tenant) error {
			close(inside)
			<-leave
			// The handles must still be open: Delete has to wait for us.
			return tn.Write(bg, func(tx *sql.Tx) error {
				_, err := tx.Exec(insertSQL, 0, 2)
				return err
			})
		})
	}()
	<-inside

	// A Delete that runs out of patience leaves the tenant alone.
	short, cancel := context.WithTimeout(bg, 50*time.Millisecond)
	err = m.Delete(short, "acme")
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) || !exists(db) {
		t.Fatalf("Delete with expired ctx = %v, file exists = %v", err, exists(db))
	}
	if n := mustQueryInt(t, m, "acme", countSQL); n != 1 {
		t.Fatalf("tenant unusable after abandoned Delete: count = %d", n)
	}

	deleted := make(chan error, 1)
	go func() { deleted <- m.Delete(bg, "acme") }()
	select {
	case err := <-deleted:
		t.Fatalf("Delete returned %v while a user was inside With", err)
	case <-time.After(200 * time.Millisecond):
	}
	if !exists(db) {
		t.Fatal("database removed while a user was inside With")
	}
	// New users are already turned away.
	if err := insert(bg, m, "acme", 0, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("With during Delete = %v, want ErrNotFound", err)
	}
	close(leave)
	if err := <-userDone; err != nil {
		t.Fatalf("in-flight user failed: %v", err)
	}
	if err := <-deleted; err != nil {
		t.Fatalf("Delete = %v", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if exists(db + suffix) {
			t.Errorf("%s still exists after Delete", db+suffix)
		}
	}
	if err := insert(bg, m, "acme", 0, 4); !errors.Is(err, ErrNotFound) {
		t.Fatalf("With after Delete = %v, want ErrNotFound", err)
	}
	if exists(db) {
		t.Fatal("With after Delete recreated the file")
	}
	if s := m.Stats(); s.Open != 0 {
		t.Fatalf("open handles after Delete = %d", s.Open)
	}

	// The ID is free again and the new tenant starts empty.
	mustCreate(t, m, "acme")
	if n := mustQueryInt(t, m, "acme", countSQL); n != 0 {
		t.Fatalf("recreated tenant has %d rows", n)
	}
}

func TestScenario03_LRU(t *testing.T) {
	const maxOpen, tenants, workers, opsPerWorker = 8, 50, 16, 150
	root := labRoot(t)
	m := newManager(t, root, maxOpen, schemaV1)
	ids := tenantIDs(tenants)
	mustCreate(t, m, ids...)

	// Independent of the manager's own counter: ask the kernel.
	var peakFiles, samples atomic.Int64
	stop, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		for {
			select {
			case <-stop:
				return
			default:
				if n, ok := openTenantFiles(t, root); ok {
					peakFiles.Store(max(peakFiles.Load(), int64(n)))
					samples.Add(1)
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()

	var want [tenants]atomic.Int64
	var failures atomic.Int64
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w), 3))
			for seq := range opsPerWorker {
				i := rng.IntN(tenants)
				err := m.With(bg, ids[i], func(tn *Tenant) error {
					err := tn.Write(bg, func(tx *sql.Tx) error {
						_, err := tx.Exec(insertSQL, w, seq)
						return err
					})
					if err != nil {
						return err
					}
					// Still inside With: an evicted handle would fail here.
					return tn.Read(bg, func(tx *sql.Tx) error {
						var n int
						return tx.QueryRow(countSQL).Scan(&n)
					})
				})
				if err != nil {
					failures.Add(1)
					t.Errorf("worker %d tenant %s: %v", w, ids[i], err)
					continue
				}
				want[i].Add(1)
			}
		})
	}
	wg.Wait()
	close(stop)
	<-sampled

	s := m.Stats()
	record(t, "03-lru", "max open (configured)", maxOpen, "tenants")
	record(t, "03-lru", "peak open (manager counter)", s.PeakOpen, "tenants")
	record(t, "03-lru", "peak open (distinct .db in /proc/self/fd)", peakFiles.Load(), "tenants")
	record(t, "03-lru", "kernel samples taken", samples.Load(), "")
	record(t, "03-lru", "evictions", s.Evictions, "")
	record(t, "03-lru", "failed operations", failures.Load(), "")
	if s.PeakOpen > maxOpen || peakFiles.Load() > maxOpen {
		t.Fatalf("open handles exceeded MaxOpen=%d: counter %d, fds %d", maxOpen, s.PeakOpen, peakFiles.Load())
	}
	if s.Evictions == 0 || samples.Load() == 0 {
		t.Fatal("no evictions or no kernel samples: the test did not exercise the LRU")
	}
	total := 0
	for i, id := range ids {
		got := mustQueryInt(t, m, id, countSQL)
		if got != int(want[i].Load()) {
			t.Errorf("%s: %d rows, want %d", id, got, want[i].Load())
		}
		total += got
	}
	if total != workers*opsPerWorker {
		t.Fatalf("total rows = %d, want %d", total, workers*opsPerWorker)
	}
}

func TestScenario03_FullCacheWaits(t *testing.T) {
	m := newManager(t, labRoot(t), 2, schemaV1)
	mustCreate(t, m, "a", "b", "c")

	leave := map[string]chan struct{}{"a": make(chan struct{}), "b": make(chan struct{})}
	var holders sync.WaitGroup
	for id, ch := range leave {
		inside := make(chan struct{})
		holders.Go(func() {
			_ = m.With(bg, id, func(*Tenant) error {
				close(inside)
				<-ch
				return nil
			})
		})
		<-inside
	}

	// Both slots are in use: a third tenant waits, and gives up with its context.
	short, cancel := context.WithTimeout(bg, 100*time.Millisecond)
	err := insert(short, m, "c", 0, 0)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("With on a full, busy cache = %v, want DeadlineExceeded", err)
	}
	// An unknown tenant does not wait for a slot, and does not cost anyone theirs.
	if err := insert(bg, m, "typo", 0, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("With(unknown) on a full cache = %v, want ErrNotFound", err)
	}
	full := m.Stats()
	if full.Open != 2 || full.PeakOpen != 2 {
		t.Fatalf("stats while full = %+v, want 2 open, peak 2", full)
	}

	// Free one slot and the waiter gets through by evicting the idle tenant.
	waiter := make(chan error, 1)
	go func() { waiter <- insert(bg, m, "c", 0, 0) }()
	time.Sleep(50 * time.Millisecond)
	close(leave["a"])
	if err := <-waiter; err != nil {
		t.Fatalf("waiter failed: %v", err)
	}
	close(leave["b"])
	holders.Wait()
	if s := m.Stats(); s.PeakOpen != 2 || s.Evictions != full.Evictions+1 {
		t.Fatalf("stats after wait = %+v, want peak 2 and one eviction more than %d", s, full.Evictions)
	}
}

func TestScenario04_ParallelIsolation(t *testing.T) {
	const tenants, writersPerTenant, writesPerWriter = 16, 4, 500
	const total = tenants * writersPerTenant * writesPerWriter
	m := newManager(t, labRoot(t), 0, schemaV1)
	ids := tenantIDs(tenants)
	mustCreate(t, m, ids...)
	mustCreate(t, m, "shared")

	// run starts the same 64 writers either spread over 16 tenants or all on one database.
	run := func(target func(writer int) string) time.Duration {
		var wg sync.WaitGroup
		start := time.Now()
		for w := range tenants * writersPerTenant {
			wg.Go(func() {
				for seq := range writesPerWriter {
					if err := insert(bg, m, target(w), w, seq); err != nil {
						t.Errorf("writer %d: %v", w, err)
					}
				}
			})
		}
		wg.Wait()
		return time.Since(start)
	}
	perTenant := run(func(w int) string { return ids[w%tenants] })
	oneDB := run(func(int) string { return "shared" })

	for _, id := range ids {
		if n := mustQueryInt(t, m, id, countSQL); n != writersPerTenant*writesPerWriter {
			t.Errorf("%s: %d rows, want %d", id, n, writersPerTenant*writesPerWriter)
		}
		if n := mustQueryInt(t, m, id, `SELECT count(DISTINCT writer) FROM events`); n != writersPerTenant {
			t.Errorf("%s: rows from %d writers, want %d", id, n, writersPerTenant)
		}
	}
	if n := mustQueryInt(t, m, "shared", countSQL); n != total {
		t.Errorf("shared: %d rows, want %d", n, total)
	}

	perTenantRate, oneDBRate := total/perTenant.Seconds(), total/oneDB.Seconds()
	record(t, "04-scaling", "writes per run (64 writers)", total, "writes")
	record(t, "04-scaling", "16 tenants x 4 writers", perTenantRate, "writes/s")
	record(t, "04-scaling", "1 database x 64 writers", oneDBRate, "writes/s")
	record(t, "04-scaling", "speedup", perTenantRate/oneDBRate, "x")
	if perTenantRate <= oneDBRate {
		t.Errorf("16 tenants (%.0f/s) did not out-write one database (%.0f/s)", perTenantRate, oneDBRate)
	}
}

func TestScenario05_NoisyNeighbor(t *testing.T) {
	const quiet, writesPerTenant, hold = 4, 300, 100 * time.Millisecond
	m := newManager(t, labRoot(t), 0, schemaV1)
	ids := tenantIDs(quiet)
	mustCreate(t, m, ids...)
	mustCreate(t, m, "noisy")

	stop, noisyDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(noisyDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			err := m.With(bg, "noisy", func(tn *Tenant) error {
				return tn.Write(bg, func(tx *sql.Tx) error {
					if _, err := tx.Exec(insertSQL, -1, 0); err != nil {
						return err
					}
					time.Sleep(hold) // holds noisy's write lock
					return nil
				})
			})
			if err != nil {
				t.Errorf("noisy: %v", err)
				return
			}
		}
	}()
	time.Sleep(hold / 2) // let the first long transaction start

	latencies := make([][]time.Duration, quiet)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Go(func() {
			for seq := range writesPerTenant {
				start := time.Now()
				if err := insert(bg, m, id, i, seq); err != nil {
					t.Errorf("%s: %v", id, err)
				}
				latencies[i] = append(latencies[i], time.Since(start))
			}
		})
	}
	wg.Wait()

	// The blast radius is the noisy tenant itself: its own writers queue.
	var sameTenant []time.Duration
	for seq := range 3 {
		start := time.Now()
		if err := insert(bg, m, "noisy", -2, seq); err != nil {
			t.Fatal(err)
		}
		sameTenant = append(sameTenant, time.Since(start))
	}
	close(stop)
	<-noisyDone

	var all []time.Duration
	for _, l := range latencies {
		all = append(all, l...)
	}
	p50, p99, worst := percentile(all, 0.50), percentile(all, 0.99), percentile(all, 1)
	record(t, "05-noisy", "neighbor write transaction", ms(hold), "ms")
	record(t, "05-noisy", "other tenants write p50", ms(p50), "ms")
	record(t, "05-noisy", "other tenants write p99", ms(p99), "ms")
	record(t, "05-noisy", "other tenants write max", ms(worst), "ms")
	record(t, "05-noisy", "same tenant write max", ms(percentile(sameTenant, 1)), "ms")
	if p99 >= hold/2 {
		t.Errorf("other tenants' write p99 = %v, want well below %v", p99, hold)
	}
	if percentile(sameTenant, 1) < hold/2 {
		t.Errorf("writes to the noisy tenant took %v: the neighbor was not holding its writer", sameTenant)
	}
}
