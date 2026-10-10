package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

const steadyCleanupTicks int64 = 250
const steadyRetentionTicks int64 = 500

func steady(o options, r *recorder) (verdict, metrics, threshold string, runErr error) {
	threshold = "bytes_per_row >= 400 over second half => GROWS; otherwise FLAT"
	if o.rows <= 0 || o.rows%40 != 0 {
		return "", "", threshold, errors.New("-rows must be a positive multiple of 40")
	}
	mode, n := "mvcc", 1
	if o.probe == "wal-steady" {
		mode = "wal"
	}
	if o.probe == "mvcc-open-reader" {
		n = 2
	}
	var db *sql.DB
	var conns []*sql.Conn
	var err error
	pragmas := ""
	if o.probe == "gosqlite-steady" {
		db, conns, pragmas, err = openGoSQLitePool(o, r)
		n = len(conns)
	} else {
		db, conns, err = openPool(o.db, mode, n)
	}
	if err != nil {
		return "", "", threshold, err
	}
	defer func() { runErr = errors.Join(runErr, closePool(db, conns)) }()
	c, ctx := conns[0], context.Background()
	if mode == "mvcc" && o.probe != "gosqlite-steady" {
		// Configure the shared MvStore before schema writes or any workload.
		pragmas, err = steadyPragmas(ctx, c, o.probe == "mvcc-forced-gc")
		if err != nil {
			return "", "", threshold, err
		}
	}
	for _, conn := range conns {
		if o.probe == "gosqlite-steady" {
			break // Keep database.DefaultConfig; do not apply raw-driver settings.
		}
		for _, q := range []string{"PRAGMA synchronous = NORMAL", "PRAGMA cache_size = -64000"} {
			if _, err := conn.ExecContext(ctx, q); err != nil {
				return "", "", threshold, err
			}
		}
	}
	for _, q := range []string{"CREATE TABLE samples(series_id INTEGER, ts INTEGER, value REAL)", "CREATE INDEX samples_series_ts ON samples(series_id, ts)", "CREATE TABLE security_events(id INTEGER PRIMARY KEY, type TEXT, source_ip TEXT, created_at TEXT)"} {
		if _, err := c.ExecContext(ctx, q); err != nil {
			return "", "", threshold, err
		}
	}
	config := fmt.Sprintf("journal_mode=%s pool_connections=%d retention_ticks=%d cleanup_ticks=%d security_events_accumulate=true", mode, n, steadyRetentionTicks, steadyCleanupTicks)
	if pragmas != "" {
		config += " " + pragmas
	}
	baseline, err := r.sample("baseline", 0, 0, n, config)
	if err != nil {
		return "", "", threshold, err
	}
	halfRows := o.rows / 2 / 40 * 40
	half, end := baseline, baseline
	var deleted, samples, events int64
	var reader *sql.Rows
	defer func() {
		if reader != nil {
			runErr = errors.Join(runErr, reader.Close())
		}
	}()
	writeTick := func(tick int64) error {
		if err := insertTick(ctx, c, tick); err != nil {
			return fmt.Errorf("tick %d: %w", tick, err)
		}
		if tick%steadyCleanupTicks == 0 {
			count, err := cleanup(ctx, c, tick-steadyRetentionTicks)
			deleted += count
			if err != nil {
				return fmt.Errorf("cleanup tick %d: %w", tick, err)
			}
		}
		return nil
	}
	counts := func(tick int64, requireDeletes bool) (string, error) {
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM samples").Scan(&samples); err != nil {
			return "", err
		}
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM security_events").Scan(&events); err != nil {
			return "", err
		}
		if err := checkSteadyCounts(tick, samples, events, deleted, requireDeletes); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s live_samples=%d security_events=%d deleted_samples=%d reader_open=%t", config, samples, events, deleted, reader != nil), nil
	}
	ticks := o.rows / 40
	started := time.Now()
	var endDetail string
	for tick := int64(1); tick <= ticks; tick++ {
		if err := writeTick(tick); err != nil {
			return "", "", threshold, err
		}
		if o.probe == "mvcc-open-reader" && tick == 100 {
			reader, err = conns[1].QueryContext(ctx, "SELECT id FROM security_events")
			if err != nil {
				return "", "", threshold, fmt.Errorf("open reader: %w", err)
			}
			// Exactly one Next leaves the native cursor and its snapshot open.
			if !reader.Next() {
				return "", "", threshold, errors.Join(errors.New("open reader returned no first row"), reader.Err())
			}
			var id int64
			if err := reader.Scan(&id); err != nil {
				return "", "", threshold, fmt.Errorf("open reader first row: %w", err)
			}
		}
		rows := tick * 40
		if rows%20000 == 0 || rows == halfRows || rows == o.rows {
			detail, err := counts(tick, rows == o.rows)
			if err != nil {
				return "", "", threshold, err
			}
			m, err := r.sample("steady", rows, 0, n, detail)
			if err != nil {
				return "", "", threshold, err
			}
			if rows == halfRows {
				half = m
			}
			if rows == o.rows {
				end, endDetail = m, detail
			}
			fmt.Fprintf(os.Stderr, "probe=%s rows=%d rss_anon_kib=%d %s\n", o.probe, rows, m.rss, detail)
		}
	}
	elapsed := time.Since(started).Seconds()
	verdict, bpr, err := steadyVerdict(halfRows, o.rows, half.rss, end.rss)
	if err != nil {
		return "", "", threshold, err
	}
	metrics = fmt.Sprintf("bytes_per_row=%.2f rss_end_mib=%.3f rows_half=%d rows_end=%d rss_half_kib=%d rss_end_kib=%d live_samples=%d security_events=%d deleted_samples=%d %s", bpr, float64(end.rss)/1024, halfRows, o.rows, half.rss, end.rss, samples, events, deleted, config)
	if o.probe == "gosqlite-steady" {
		metrics += fmt.Sprintf(" elapsed_s=%.6f inserts_per_s=%.3f", elapsed, float64(o.rows)/elapsed)
	}
	if o.trim {
		trimmed, err := r.trimSample("rss_after_trim", o.rows, 0, n, endDetail)
		if err != nil {
			return "", "", threshold, err
		}
		metrics += fmt.Sprintf(" rss_after_trim_mib=%.3f rss_after_trim_kib=%d", float64(trimmed.rss)/1024, trimmed.rss)
	}
	if o.probe == "mvcc-open-reader" {
		if reader == nil {
			return "", "", threshold, errors.New("workload ended before open reader at tick 100")
		}
		if err := reader.Close(); err != nil {
			return "", "", threshold, fmt.Errorf("release reader: %w", err)
		}
		reader = nil
		for tick := ticks + 1; tick <= ticks+steadyCleanupTicks; tick++ {
			if err := writeTick(tick); err != nil {
				return "", "", threshold, err
			}
		}
		releaseRows := (ticks + steadyCleanupTicks) * 40
		detail, err := counts(ticks+steadyCleanupTicks, true)
		if err != nil {
			return "", "", threshold, err
		}
		m, err := r.sample("after_release", releaseRows, 0, n, detail)
		if err != nil {
			return "", "", threshold, err
		}
		metrics += fmt.Sprintf(" rss_after_release_mib=%.3f rss_after_release_kib=%d rows_after_release=%d live_samples_after_release=%d deleted_samples_after_release=%d security_events_after_release=%d", float64(m.rss)/1024, m.rss, releaseRows, samples, deleted, events)
		if o.trim {
			trimmed, err := r.trimSample("rss_after_release_trim", releaseRows, 0, n, detail)
			if err != nil {
				return "", "", threshold, err
			}
			metrics += fmt.Sprintf(" rss_after_release_trim_mib=%.3f rss_after_release_trim_kib=%d", float64(trimmed.rss)/1024, trimmed.rss)
		}
	}
	return verdict, metrics, threshold, nil
}

func steadyPragmas(ctx context.Context, c *sql.Conn, forced bool) (string, error) {
	var readbacks []string
	for _, p := range []struct {
		name string
		want int64
	}{{"mvcc_checkpoint_threshold", 0}, {"mvcc_gc_threshold", 1}} {
		if forced {
			q := fmt.Sprintf("PRAGMA %s = %d", p.name, p.want)
			if _, err := c.ExecContext(ctx, q); err != nil {
				return "", fmt.Errorf("forced GC %s: %w", q, err)
			}
		}
		var actual int64
		if err := c.QueryRowContext(ctx, "PRAGMA "+p.name).Scan(&actual); err != nil {
			return "", fmt.Errorf("MVCC readback %s: %w", p.name, err)
		}
		if forced && actual != p.want {
			return "", fmt.Errorf("forced GC readback %s=%d, want %d", p.name, actual, p.want)
		}
		readbacks = append(readbacks, fmt.Sprintf("%s=%d", p.name, actual))
	}
	return strings.Join(readbacks, " "), nil
}

func checkSteadyCounts(ticks, samples, events, deleted int64, requireDeletes bool) error {
	lastCleanup := ticks / steadyCleanupTicks * steadyCleanupTicks
	expiredTicks := max(int64(0), lastCleanup-steadyRetentionTicks-1)
	wantDeleted := expiredTicks * 40
	wantSamples := ticks*40 - wantDeleted
	if samples != wantSamples || events != ticks || deleted != wantDeleted {
		return fmt.Errorf("steady row counts live_samples=%d want=%d security_events=%d want=%d deleted_samples=%d want=%d", samples, wantSamples, events, ticks, deleted, wantDeleted)
	}
	if requireDeletes && deleted == 0 {
		return fmt.Errorf("deleted_samples=0 after %d ticks; run at least 30000 sample rows to exercise retention", ticks)
	}
	return nil
}

func steadyVerdict(halfRows, endRows, halfRSS, endRSS int64) (string, float64, error) {
	if halfRows < 0 || endRows <= halfRows || halfRSS < 0 || endRSS <= 0 {
		return "", 0, errors.New("invalid steady raw measurements")
	}
	bpr := float64(endRSS-halfRSS) * 1024 / float64(endRows-halfRows)
	if bpr >= 400 {
		return "GROWS", bpr, nil
	}
	return "FLAT", bpr, nil
}
