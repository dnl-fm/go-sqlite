package tenants

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const (
	scaleTenants   = 1000
	scaleWorkers   = 16
	randomDuration = 60 * time.Second
)

// scaleStep is migration step n for the MigrateAll timings. Every step has the
// same shape, so the three concurrency levels do the same work.
func scaleStep(n int) string {
	return fmt.Sprintf(`CREATE TABLE m%[1]d (id INTEGER PRIMARY KEY, a TEXT NOT NULL); CREATE INDEX m%[1]d_a ON m%[1]d (a)`, n)
}

func TestScenario10_Scale(t *testing.T) {
	if !full {
		t.Skip("full mode only: LAB_FULL=1")
	}
	root := labRoot(t)
	ids := tenantIDs(scaleTenants)
	steps := []string{schemaV1, scaleStep(2), scaleStep(3), scaleStep(4)}

	t.Run("create", func(t *testing.T) {
		m := newManager(t, root, 0, steps[0])
		start := time.Now()
		lat := parallel(scaleWorkers, ids, func(id string) error { return m.Create(bg, id) }, t)
		wall := time.Since(start)
		record(t, "10-create", "tenants created", scaleTenants, "tenants")
		record(t, "10-create", fmt.Sprintf("wall, %d creators", scaleWorkers), wall.Seconds(), "s")
		record(t, "10-create", "create p50", ms(percentile(lat, 0.50)), "ms")
		record(t, "10-create", "create p99", ms(percentile(lat, 0.99)), "ms")
		// The most recently created tenant is still open: WAL and shm exist.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			info, err := os.Stat(filepath.Join(root, ids[scaleTenants-1]+".db"+suffix))
			if err != nil {
				t.Fatal(err)
			}
			record(t, "10-create", "disk, one open empty tenant: .db"+suffix, info.Size(), "bytes")
		}
		if err := m.Close(); err != nil {
			t.Fatal(err)
		}
		size, blocks := diskUsage(t, root)
		record(t, "10-create", "disk per empty tenant, closed: file size", size/scaleTenants, "bytes")
		record(t, "10-create", "disk per empty tenant, closed: allocated", blocks/scaleTenants, "bytes")
	})

	// Memory and descriptors are per-process high-water marks, so each cache
	// size is measured in a fresh process.
	for _, maxOpen := range []int{10, 100, 1000} {
		t.Run(fmt.Sprintf("open-%d", maxOpen), func(t *testing.T) {
			runChild(t, "TestScaleOpenChild", "open", root, childMaxOpenEnv+"="+strconv.Itoa(maxOpen))
		})
	}

	t.Run("fds-busy", func(t *testing.T) {
		m := newManager(t, root, 0, steps[0])
		prefix := filepath.Join(root, ids[0]+".db")
		count := func() (n int) {
			for _, target := range openFiles(t) {
				if strings.HasPrefix(target, prefix) {
					n++
				}
			}
			return n
		}
		err := m.With(bg, ids[0], func(tn *Tenant) error {
			record(t, "10-fds", "fds, tenant open, writer only", count(), "fds")
			ready, leave := make(chan struct{}), make(chan struct{})
			var wg sync.WaitGroup
			for range readerConns {
				wg.Go(func() {
					_ = tn.Read(bg, func(tx *sql.Tx) error {
						var n int
						err := tx.QueryRow(countSQL).Scan(&n)
						ready <- struct{}{}
						<-leave
						return err
					})
				})
			}
			for range readerConns {
				<-ready
			}
			record(t, "10-fds", fmt.Sprintf("fds, writer + %d busy readers (peak)", readerConns), count(), "fds")
			close(leave)
			wg.Wait()
			time.Sleep(100 * time.Millisecond)
			record(t, "10-fds", "fds, after the readers go idle", count(), "fds")
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	})

	for _, dist := range []string{"uniform", "zipf-1.1"} {
		t.Run("random-"+dist, func(t *testing.T) {
			scenario := "10-random-" + dist
			m := newManager(t, root, 64, steps[0])
			ctx, cancel := context.WithTimeout(bg, randomDuration)
			defer cancel()
			lat := make([][]time.Duration, scaleWorkers)
			var failures atomic.Int64
			var wg sync.WaitGroup
			start := time.Now()
			for w := range scaleWorkers {
				wg.Go(func() {
					rng := rand.New(rand.NewPCG(uint64(w), 10))
					zipf := rand.NewZipf(rng, 1.1, 1, scaleTenants-1)
					for seq := 0; ctx.Err() == nil; seq++ {
						i := rng.IntN(scaleTenants)
						if dist != "uniform" {
							i = int(zipf.Uint64())
						}
						// One request: open the tenant, read, write.
						begin := time.Now()
						err := m.With(bg, ids[i], func(tn *Tenant) error {
							err := tn.Read(bg, func(tx *sql.Tx) error {
								var n int
								return tx.QueryRow(countSQL).Scan(&n)
							})
							if err != nil {
								return err
							}
							return tn.Write(bg, func(tx *sql.Tx) error {
								_, err := tx.Exec(insertSQL, w, seq)
								return err
							})
						})
						if err != nil {
							failures.Add(1)
							t.Errorf("%s: %v", ids[i], err)
							return
						}
						lat[w] = append(lat[w], time.Since(begin))
					}
				})
			}
			wg.Wait()
			wall := time.Since(start)
			var all []time.Duration
			for _, l := range lat {
				all = append(all, l...)
			}
			s := m.Stats()
			record(t, scenario, "tenants / MaxOpen / workers", fmt.Sprintf("%d / 64 / %d", scaleTenants, scaleWorkers), "")
			record(t, scenario, "duration", wall.Seconds(), "s")
			record(t, scenario, "requests (1 read tx + 1 write tx)", len(all), "requests")
			record(t, scenario, "throughput", float64(len(all))/wall.Seconds(), "requests/s")
			record(t, scenario, "cache hit rate", 100*float64(s.Hits)/float64(s.Hits+s.Misses), "%")
			record(t, scenario, "evictions", s.Evictions, "")
			record(t, scenario, "request p50", ms(percentile(all, 0.50)), "ms")
			record(t, scenario, "request p99", ms(percentile(all, 0.99)), "ms")
			record(t, scenario, "request max", ms(percentile(all, 1)), "ms")
			record(t, scenario, "peak open", s.PeakOpen, "tenants")
			record(t, scenario, "failed requests", failures.Load(), "")
			if s.PeakOpen > 64 {
				t.Fatalf("peak open %d exceeded MaxOpen 64", s.PeakOpen)
			}
		})
	}

	for i, concurrency := range []int{1, 8, 32} {
		t.Run(fmt.Sprintf("migrate-all-%d", concurrency), func(t *testing.T) {
			m := newManager(t, root, 0, steps[:i+2]...)
			start := time.Now()
			results, err := m.MigrateAll(bg, concurrency)
			wall := time.Since(start)
			if err != nil || len(results) != scaleTenants {
				t.Fatalf("MigrateAll = %d results, %v", len(results), err)
			}
			for _, r := range results {
				if r.Err != nil {
					t.Fatalf("%s: %v", r.ID, r.Err)
				}
			}
			if v := mustQueryInt(t, m, ids[scaleTenants/2], "PRAGMA user_version"); v != i+2 {
				t.Fatalf("version = %d, want %d", v, i+2)
			}
			record(t, "10-migrate", fmt.Sprintf("MigrateAll, 1 step, concurrency %d: wall", concurrency), wall.Seconds(), "s")
			record(t, "10-migrate", fmt.Sprintf("MigrateAll, 1 step, concurrency %d: rate", concurrency), scaleTenants/wall.Seconds(), "tenants/s")
		})
	}

	t.Run("migrate-all-noop", func(t *testing.T) {
		m := newManager(t, root, 0, steps...)
		start := time.Now()
		results, err := m.MigrateAll(bg, 32)
		wall := time.Since(start)
		if err != nil || len(results) != scaleTenants {
			t.Fatalf("MigrateAll = %d results, %v", len(results), err)
		}
		for _, r := range results {
			if r.Err != nil {
				t.Fatalf("%s: %v", r.ID, r.Err)
			}
		}
		record(t, "10-migrate", "MigrateAll, nothing to do, concurrency 32: wall", wall.Seconds(), "s")
	})
}

// TestScaleOpenChild measures one cache size against the 1,000 tenants its
// parent, TestScenario10_Scale, created.
func TestScaleOpenChild(t *testing.T) {
	if os.Getenv(childEnv) != "open" {
		t.Skip("runs as a child of TestScenario10_Scale")
	}
	maxOpen, err := strconv.Atoi(os.Getenv(childMaxOpenEnv))
	if err != nil {
		t.Fatal(err)
	}
	ids := tenantIDs(scaleTenants)
	scenario := fmt.Sprintf("10-open-%d", maxOpen)
	m := newManager(t, os.Getenv(childRootEnv), maxOpen, schemaV1)
	baseRSS, baseFDs := rssAnon(t), len(openFiles(t))
	touchAll := func(ids []string) (lat []time.Duration) {
		for _, id := range ids {
			start := time.Now()
			if _, err := queryInt(bg, m, id, countSQL); err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			lat = append(lat, time.Since(start))
		}
		return lat
	}
	latency := func(metric string, lat []time.Duration) {
		record(t, scenario, metric+" p50", ms(percentile(lat, 0.50)), "ms")
		record(t, scenario, metric+" p99", ms(percentile(lat, 0.99)), "ms")
	}
	perTenant := func(metric string, total int, unit string) {
		record(t, scenario, metric, float64(total)/float64(maxOpen), unit)
	}

	// Fill the cache: cold opens with nothing to evict.
	cold := touchAll(ids[:maxOpen])
	readRSS, readFDs := rssAnon(t), len(openFiles(t))
	record(t, scenario, "tenants open at once", maxOpen, "tenants")
	record(t, scenario, "RssAnon, nothing open", baseRSS, "kB")
	record(t, scenario, "RssAnon, all open, each read once", readRSS, "kB")
	perTenant("RssAnon per open tenant, each read once", readRSS-baseRSS, "kB")
	perTenant("fds per open tenant, each read once", readFDs-baseFDs, "fds")
	latency("cold open + first query, cache has room", cold)

	// Past the cap, every cold open first closes the least recently used
	// tenant. These victims were only read: nothing to checkpoint.
	if maxOpen < scaleTenants {
		latency("cold open + first query, evicting a read-only tenant", touchAll(ids[maxOpen:]))
	}
	cached := ids[scaleTenants-maxOpen:]
	for _, id := range cached {
		if err := insert(bg, m, id, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	writeRSS, writeFDs := rssAnon(t), len(openFiles(t))
	record(t, scenario, "RssAnon, all open, each written once", writeRSS, "kB")
	perTenant("RssAnon per open tenant, each written once", writeRSS-baseRSS, "kB")
	perTenant("fds per open tenant, each written once", writeFDs-baseFDs, "fds")
	var warm []time.Duration
	for len(warm) < scaleTenants {
		warm = append(warm, touchAll(cached)...)
	}
	latency("warm query", warm)

	// Now the victims have a WAL: closing one checkpoints and fsyncs.
	if maxOpen < scaleTenants {
		latency("cold open + first query, evicting a written tenant", touchAll(ids[:maxOpen]))
	}
	if s := m.Stats(); s.Open != maxOpen || s.PeakOpen != maxOpen {
		t.Fatalf("stats = %+v, want %d open and peak", s, maxOpen)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	record(t, scenario, "RssAnon, after Close", rssAnon(t), "kB")
}

// parallel runs fn for every id on n goroutines and returns each call's latency.
func parallel(n int, ids []string, fn func(id string) error, t *testing.T) []time.Duration {
	lat := make([]time.Duration, len(ids))
	var next atomic.Int64
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for i := int(next.Add(1)) - 1; i < len(ids); i = int(next.Add(1)) - 1 {
				start := time.Now()
				if err := fn(ids[i]); err != nil {
					t.Errorf("%s: %v", ids[i], err)
				}
				lat[i] = time.Since(start)
			}
		})
	}
	wg.Wait()
	return lat
}

// diskUsage sums file sizes and allocated bytes of everything in dir.
func diskUsage(t *testing.T, dir string) (size, allocated int64) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		size += info.Size()
		allocated += info.Sys().(*syscall.Stat_t).Blocks * 512
	}
	return size, allocated
}
