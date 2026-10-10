package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func growth(o options, r *recorder) (verdict, metrics, threshold string, runErr error) {
	threshold = "bytes_per_row >= 400 (growth only; see mvcc-retention for WAL comparison)"
	if o.rows <= 0 || o.rows%40 != 0 {
		return "", "", threshold, errors.New("-rows must be a positive multiple of 40")
	}
	mode := "mvcc"
	n := 4
	if o.probe == "wal-growth" {
		mode = "wal"
	}
	if o.probe == "mvcc-growth-1conn" {
		n = 1
	}
	db, conns, err := openPool(o.db, mode, n)
	if err != nil {
		return "", "", threshold, err
	}
	defer func() { runErr = errors.Join(runErr, closePool(db, conns)) }()
	// Keep the other initialized connections idle and open for the pool-pinning control.
	c := conns[0]
	ctx := context.Background()
	for _, conn := range conns {
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
	baseline, err := r.sample("baseline", 0, 0, 0, fmt.Sprintf("journal_mode=%s pool_connections=%d", mode, n))
	if err != nil {
		return "", "", threshold, err
	}
	halfRows := (o.rows / 2 / 40) * 40
	half, end := baseline, baseline
	var deleted int64
	for tick := int64(1); tick <= o.rows/40; tick++ {
		if err := insertTick(ctx, c, tick); err != nil {
			return "", "", threshold, fmt.Errorf("tick %d: %w", tick, err)
		}
		rows := tick * 40
		if tick%2000 == 0 {
			count, err := cleanup(ctx, c, tick-20000)
			deleted += count
			if err != nil {
				return "", "", threshold, err
			}
		}
		detail := fmt.Sprintf("deleted_samples=%d", deleted)
		if o.probe == "mvcc-growth-checkpoint" && rows%20000 == 0 {
			detail += " " + checkpoint(ctx, c)
		}
		if rows%20000 == 0 || rows == halfRows || rows == o.rows {
			m, err := r.sample("growth", rows, 0, 0, detail)
			if err != nil {
				return "", "", threshold, err
			}
			if rows == halfRows {
				half = m
			}
			if rows == o.rows {
				end = m
			}
			fmt.Fprintf(os.Stderr, "probe=%s rows=%d rss_anon_kib=%d %s\n", o.probe, rows, m.rss, detail)
		}
	}
	var samples, events int64
	if err := c.QueryRowContext(ctx, "SELECT count(*) FROM samples").Scan(&samples); err != nil {
		return "", "", threshold, err
	}
	if err := c.QueryRowContext(ctx, "SELECT count(*) FROM security_events").Scan(&events); err != nil {
		return "", "", threshold, err
	}
	if samples != o.rows-deleted || events != o.rows/40 {
		return "", "", threshold, fmt.Errorf("row counts samples=%d events=%d deleted=%d", samples, events, deleted)
	}
	bpr := float64(end.rss-half.rss) * 1024 / float64(o.rows-halfRows)
	metrics = fmt.Sprintf("bytes_per_row=%.2f rss_end_mib=%.3f rows_half=%d rows_end=%d rss_half_kib=%d rss_end_kib=%d deleted_samples=%d pool_connections=%d", bpr, float64(end.rss)/1024, halfRows, o.rows, half.rss, end.rss, deleted, n)
	return reproduced(bpr >= 400), metrics, threshold, nil
}
func insertTick(ctx context.Context, c *sql.Conn, tick int64) error {
	tx, err := c.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for series := 0; series < 40; series++ {
		if _, err := tx.ExecContext(ctx, "INSERT INTO samples(series_id, ts, value) VALUES (?, ?, ?)", series, tick, float64(tick%1000)+float64(series)/40); err != nil {
			return err
		}
	}
	created := time.Unix(tick*15, 0).UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, "INSERT INTO security_events(id, type, source_ip, created_at) VALUES (?, ?, ?, ?)", tick, "connection", "192.0.2.1", created); err != nil {
		return err
	}
	return tx.Commit()
}
func cleanup(ctx context.Context, c *sql.Conn, lastExpiredTick int64) (int64, error) {
	var total int64
	for {
		result, err := c.ExecContext(ctx, "DELETE FROM samples WHERE rowid IN (SELECT rowid FROM samples WHERE ts < ? LIMIT 5000)", lastExpiredTick)
		if err != nil {
			return total, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < 5000 {
			return total, nil
		}
	}
}
func checkpoint(ctx context.Context, c *sql.Conn) string {
	rows, err := c.QueryContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	if err != nil {
		return "checkpoint_error=" + err.Error()
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return "checkpoint_error=" + err.Error()
	}
	var result []string
	for rows.Next() {
		values := make([]any, len(columns))
		ptrs := make([]any, len(columns))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return "checkpoint_error=" + err.Error()
		}
		for i, v := range values {
			result = append(result, fmt.Sprintf("%s=%v", columns[i], v))
		}
	}
	if err := rows.Err(); err != nil {
		return "checkpoint_error=" + err.Error()
	}
	if err := rows.Close(); err != nil {
		return "checkpoint_error=" + err.Error()
	}
	return "checkpoint_result=[" + strings.Join(result, ",") + "]"
}
func growthVerdicts(out string) error {
	f, err := os.Open(filepath.Join(out, "summary.tsv"))
	if err != nil {
		return err
	}
	defer f.Close()
	type result struct {
		verdict string
		values  map[string]int64
	}
	results := map[string]result{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Split(scanner.Text(), "\t")
		if len(fields) < 3 {
			continue
		}
		r := result{verdict: fields[1], values: map[string]int64{}}
		for _, metric := range strings.Fields(fields[2]) {
			key, value, ok := strings.Cut(metric, "=")
			if ok {
				if n, err := strconv.ParseInt(value, 10, 64); err == nil {
					r.values[key] = n
				}
			}
		}
		results[fields[0]] = r
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	for _, pair := range [][2]string{{"mvcc-growth", "mvcc-retention"}, {"mvcc-growth-checkpoint", "mvcc-retention-checkpoint"}, {"mvcc-growth-1conn", "mvcc-retention-1conn"}} {
		mvcc, wal := results[pair[0]], results["wal-growth"]
		halfRows, hok := mvcc.values["rows_half"]
		endRows, eok := mvcc.values["rows_end"]
		halfRSS, hsok := mvcc.values["rss_half_kib"]
		endRSS, esok := mvcc.values["rss_end_kib"]
		walRSS, wok := wal.values["rss_end_kib"]
		validVerdict := func(v string) bool { return v == "REPRODUCED" || v == "NOT_REPRODUCED" }
		threshold := "bytes_per_row >= 400 AND mvcc_rss_end >= 4 * wal_rss_end"
		if !hok || !eok || !hsok || !esok || !wok || halfRows < 0 || endRows <= halfRows || halfRSS < 0 || endRSS <= 0 || walRSS <= 0 || !validVerdict(mvcc.verdict) || !validVerdict(wal.verdict) {
			err = summary(out, pair[1], "ERROR", "message=growth probe or WAL control failed or has invalid raw measurements", threshold)
		} else {
			// Decisions use raw measurements. Rounded presentation values can cross a threshold.
			bpr := float64(endRSS-halfRSS) * 1024 / float64(endRows-halfRows)
			ratio := float64(endRSS) / float64(walRSS)
			err = summary(out, pair[1], reproduced(bpr >= 400 && ratio >= 4), fmt.Sprintf("bytes_per_row=%.2f mvcc_rss_end_mib=%.3f wal_rss_end_mib=%.3f rss_ratio=%.3f", bpr, float64(endRSS)/1024, float64(walRSS)/1024, ratio), threshold)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
