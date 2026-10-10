package writers

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The wal-growth load is deliberately modest, about 1,500 commits per second
// from 4 writers, so that a WAL that never resets grows to hundreds of
// megabytes in a minute instead of filling the disk.
const (
	walDuration   = 60 * time.Second
	walWriters    = 4
	walWritePause = 2 * time.Millisecond
)

type walVariant struct {
	name            string
	open            func(path string) (*target, error)
	readPause       time.Duration
	heldReader      bool          // one read transaction stays open for the whole run
	checkpointEvery time.Duration // run PRAGMA wal_checkpoint(TRUNCATE) on a timer
}

func walVariants() []walVariant {
	split := func(p string) (*target, error) { return openSplit(p, "NORMAL", false) }
	immediate := func(p string) (*target, error) { return openNaive(p, true) }
	noAuto := func(p string) (*target, error) { return openNaive(p, true, "wal_autocheckpoint=0") }
	return []walVariant{
		{name: "C split", open: split, readPause: scenarioReadPause},
		{name: "C split, readers never pause", open: split},
		{name: "C split, one read tx held open", open: split, readPause: scenarioReadPause, heldReader: true},
		{name: "B immediate (4 writer conns)", open: immediate, readPause: scenarioReadPause},
		{name: "B immediate + TRUNCATE checkpoint every 5s", open: immediate, readPause: scenarioReadPause, checkpointEvery: 5 * time.Second},
		{name: "B immediate, wal_autocheckpoint=0 + TRUNCATE checkpoint every 5s", open: noAuto, readPause: scenarioReadPause, checkpointEvery: 5 * time.Second},
	}
}

// walRow is one line of the wal-growth table.
type walRow struct {
	Name                     string
	Ops, Busy, Other         int
	Elapsed                  time.Duration
	WP99, RP99               time.Duration
	Series                   []int64 // -wal size, one sample per second
	Max, End, AfterTruncate  int64
	Checkpoints, CheckpointB int    // timed checkpoints run, and how many reported busy
	HeldCheckpoint           string // what a checkpoint did while the read tx was still open
}

var walResults []walRow

// checkpoint runs a TRUNCATE checkpoint and reports whether SQLite could not
// finish it.
func checkpoint(db *sql.DB) (busy bool, err error) {
	var b, log, done int
	err = db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&b, &log, &done)
	return b != 0, err
}

func runWALVariant(t *testing.T, v walVariant) walRow {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(labDir(t), "lab.db")
	tg, err := v.open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer tg.close()
	err = tg.write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(schema)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r := walRow{Name: v.name}

	var held *sql.Conn
	if v.heldReader {
		held, err = tg.rdb.Conn(ctx)
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
	}

	done := make(chan sample, 1)
	go func() {
		done <- runPlan(plan{
			Workload: "wal", Writers: walWriters, Readers: scenarioReaders, Duration: walDuration,
			ReadPause: v.readPause, WritePause: walWritePause,
		}, tg, dbPath)
	}()

	second := time.NewTicker(time.Second)
	defer second.Stop()
	var s sample
	began := time.Now()
	nextCheckpoint := v.checkpointEvery
sampling:
	for {
		select {
		case s = <-done:
			break sampling
		case <-second.C:
			r.Series = append(r.Series, walSize(dbPath))
			if v.checkpointEvery > 0 && time.Since(began) >= nextCheckpoint {
				nextCheckpoint += v.checkpointEvery
				busy, err := checkpoint(tg.wdb)
				if err != nil {
					t.Fatal(err)
				}
				r.Checkpoints++
				if busy {
					r.CheckpointB++
				}
			}
		}
	}
	r.Max, r.End = max(slices.Max(r.Series), s.WALBytes), s.WALBytes

	if held != nil {
		// With the load gone and nothing else in the way, a checkpoint still
		// cannot reclaim the WAL while the old snapshot is alive.
		busy, err := checkpoint(tg.wdb)
		if err != nil {
			t.Fatal(err)
		}
		r.HeldCheckpoint = fmt.Sprintf("busy=%t, wal still %s MB", busy, mb(walSize(dbPath)))
		_, err = held.ExecContext(ctx, "ROLLBACK")
		if err != nil {
			t.Fatal(err)
		}
	}
	busy, err := checkpoint(tg.wdb)
	if err != nil || busy {
		t.Fatalf("%s: final checkpoint: busy=%t err=%v", v.name, busy, err)
	}
	r.AfterTruncate = walSize(dbPath)

	slices.Sort(s.WriteNS)
	slices.Sort(s.ReadNS)
	r.Ops, r.Busy, r.Other, r.Elapsed = s.Ops, s.Busy, s.Other, s.Elapsed
	r.WP99, r.RP99 = percentile(s.WriteNS, 0.99), percentile(s.ReadNS, 0.99)
	if s.FirstBusy != "" || s.FirstOther != "" {
		t.Logf("%s: first errors: %s %s", v.name, s.FirstBusy, s.FirstOther)
	}
	if landed := countItems(t, tg.rdb); landed != s.Ops {
		t.Errorf("%s: %d acknowledged writes, %d rows", v.name, s.Ops, landed)
	}
	return r
}

// TestScenarioWALGrowth watches the -wal file for a minute per variant. The
// assertions pin the operational rules of the split pattern. The B variants
// are data: they stand for any setup with several writer connections on one
// file, which is also what the multi-process strategy is.
func TestScenarioWALGrowth(t *testing.T) {
	if !fullMode() {
		t.Skip("wal-growth runs in full mode only (LAB_FULL=1)")
	}
	const bounded = 16 << 20 // default wal_autocheckpoint is 1000 pages, about 4 MB
	for _, v := range walVariants() {
		t.Run(v.name, func(t *testing.T) {
			r := runWALVariant(t, v)
			walResults = append(walResults, r)
			if r.AfterTruncate != 0 {
				t.Errorf("wal is %d bytes after the final TRUNCATE checkpoint, want 0", r.AfterTruncate)
			}
			switch v.name {
			case "C split":
				if r.Busy+r.Other != 0 || r.Max > bounded {
					t.Errorf("split pattern: %d errors, wal peaked at %s MB; want none and a bounded wal", r.Busy+r.Other, mb(r.Max))
				}
			case "C split, one read tx held open":
				if r.Max <= bounded {
					t.Errorf("held read tx: wal peaked at only %s MB; starvation was not demonstrated", mb(r.Max))
				}
			}
		})
	}
	t.Log("\n" + walTable(walResults))
}

// walTable renders the wal-growth rows as a markdown table. Sizes are MB.
func walTable(rows []walRow) string {
	var b strings.Builder
	b.WriteString("| variant | writes/s | errors | write p99 ms | read p99 ms | wal @10s | @20s | @30s | @40s | @50s | wal max | wal at end | after TRUNCATE | notes |\n")
	b.WriteString("|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %.0f | %d | %s | %s |", r.Name, float64(r.Ops)/r.Elapsed.Seconds(), r.Busy+r.Other, ms(r.WP99), ms(r.RP99))
		for _, at := range []int{10, 20, 30, 40, 50} {
			if at <= len(r.Series) {
				fmt.Fprintf(&b, " %s |", mb(r.Series[at-1]))
			} else {
				b.WriteString(" |")
			}
		}
		var notes []string
		if r.Checkpoints > 0 {
			notes = append(notes, fmt.Sprintf("%d timed checkpoints, %d busy", r.Checkpoints, r.CheckpointB))
		}
		if r.HeldCheckpoint != "" {
			notes = append(notes, "checkpoint with the read tx still open: "+r.HeldCheckpoint)
		}
		fmt.Fprintf(&b, " %s | %s | %s | %s |\n", mb(r.Max), mb(r.End), mb(r.AfterTruncate), strings.Join(notes, "; "))
	}
	return b.String()
}
