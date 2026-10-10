package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

func build(o options, r *recorder) (verdict, metrics, threshold string, runErr error) {
	threshold = "fixture built to at least size_mb MiB; NOT_REPRODUCED means fixture success"
	if o.sizeMB <= 0 {
		return "", "", threshold, errors.New("-size-mb must be positive")
	}
	db, conns, err := openPool(o.db, "mvcc", 1)
	if err != nil {
		return "", "", threshold, err
	}
	closed := false
	defer func() {
		if !closed {
			runErr = errors.Join(runErr, closePool(db, conns))
		}
	}()
	ctx := context.Background()
	c := conns[0]
	for _, q := range []string{"CREATE TABLE docs(id INTEGER PRIMARY KEY, lookup TEXT, body TEXT)", "CREATE INDEX docs_lookup ON docs(lookup)"} {
		if _, err := c.ExecContext(ctx, q); err != nil {
			return "", "", threshold, err
		}
	}
	if _, err := r.sample("baseline", 0, 0, 0, "journal_mode=mvcc"); err != nil {
		return "", "", threshold, err
	}
	target := int64(o.sizeMB) * 1024 * 1024
	rng := rand.New(rand.NewPCG(17, 43))
	const alphabet = "abcdefghijklmnopqrstuvwxyz" + "ABCDEFGHIJKLMNOPQRSTUVWXYZ" + "0123456789"
	body := make([]byte, 2048)
	var count, logicalSize int64
	for logicalSize < target {
		tx, err := c.BeginTx(ctx, nil)
		if err != nil {
			return "", "", threshold, err
		}
		for i := 0; i < 1000; i++ {
			for j := range body {
				body[j] = alphabet[rng.IntN(len(alphabet))]
			}
			count++
			if _, err := tx.ExecContext(ctx, "INSERT INTO docs(id, lookup, body) VALUES (?, ?, ?)", count, fmt.Sprintf("key-%012d", count), string(body)); err != nil {
				_ = tx.Rollback()
				return "", "", threshold, err
			}
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return "", "", threshold, err
		}
		var pages, pageSize int64
		if err := c.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
			return "", "", threshold, err
		}
		if err := c.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
			return "", "", threshold, err
		}
		logicalSize = pages * pageSize
	}
	if err := closePool(db, conns); err != nil {
		closed = true
		return "", "", threshold, err
	}
	closed = true
	stat, err := os.Stat(o.db)
	if err != nil {
		return "", "", threshold, err
	}
	if stat.Size() < target {
		return "", "", threshold, fmt.Errorf("closed database size=%d, want at least %d", stat.Size(), target)
	}
	if _, err := r.sample("closed", count, 0, 0, fmt.Sprintf("db_size_bytes=%d", stat.Size())); err != nil {
		return "", "", threshold, err
	}
	fmt.Printf("database=%s rows=%d db_size_bytes=%d\n", o.db, count, stat.Size())
	return "NOT_REPRODUCED", fmt.Sprintf("db_size_mib=%.3f db_size_bytes=%d rows=%d", float64(stat.Size())/(1024*1024), stat.Size(), count), threshold, nil
}
func vacuum(o options, r *recorder) (verdict, metrics, threshold string, runErr error) {
	threshold = "retained_ratio >= 1.0"
	if o.passes <= 0 {
		return "", "", threshold, errors.New("-passes must be positive")
	}
	stat, err := os.Stat(o.db)
	if err != nil {
		return "", "", threshold, err
	}
	if stat.Size() == 0 {
		return "", "", threshold, errors.New("empty fixture")
	}
	db, conns, err := openPool(o.db, "mvcc", 1)
	if err != nil {
		return "", "", threshold, err
	}
	defer func() { runErr = errors.Join(runErr, closePool(db, conns)) }()
	ctx := context.Background()
	c := conns[0]
	var count int64
	if err := c.QueryRowContext(ctx, "SELECT count(*) FROM docs").Scan(&count); err != nil {
		return "", "", threshold, err
	}
	baseline, err := r.sample("baseline", count, 0, 0, fmt.Sprintf("db_size_bytes=%d journal_mode=mvcc", stat.Size()))
	if err != nil {
		return "", "", threshold, err
	}
	var first, maxRatio, last float64
	var end, afterTrim measurement
	for pass := 1; pass <= o.passes; pass++ {
		targetDir, err := os.MkdirTemp(filepath.Dir(o.db), "vacuum-")
		if err != nil {
			return "", "", threshold, err
		}
		target := filepath.Join(targetDir, "copy.db")
		_, vacuumErr := c.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(target, "'", "''")+"'")
		removeErr := os.RemoveAll(targetDir)
		if err := errors.Join(vacuumErr, removeErr); err != nil {
			return "", "", threshold, fmt.Errorf("pass %d: %w", pass, err)
		}
		m, err := r.sample("after_vacuum", count, pass, 0, "")
		if err != nil {
			return "", "", threshold, err
		}
		last = float64(m.rss-baseline.rss) * 1024 / float64(stat.Size())
		end = m
		if pass == 1 {
			first = last
			maxRatio = last
		}
		if last > maxRatio {
			maxRatio = last
		}
		fmt.Fprintf(os.Stderr, "probe=vacuum-into pass=%d rss_anon_kib=%d ratio=%.3f\n", pass, m.rss, last)
		if o.trim {
			afterTrim, err = r.trimSample("after_vacuum_trim", count, pass, 0, "")
			if err != nil {
				return "", "", threshold, err
			}
		}
	}
	metrics = fmt.Sprintf("retained_ratio=%.3f first_pass_ratio=%.3f max_ratio=%.3f db_size_bytes=%d", last, first, maxRatio, stat.Size())
	if o.trim {
		metrics += fmt.Sprintf(" rss_end_mib=%.3f rss_after_trim_mib=%.3f", float64(end.rss)/1024, float64(afterTrim.rss)/1024)
	}
	return reproduced(last >= 1), metrics, threshold, nil
}
func cache(o options, r *recorder) (verdict, metrics, threshold string, runErr error) {
	threshold = "delta_mib > conns * cache_kib / 1024 + 64"
	if o.conns <= 0 || o.cacheKiB <= 0 {
		return "", "", threshold, errors.New("-conns and -cache-kib must be positive")
	}
	stat, err := os.Stat(o.db)
	if err != nil {
		return "", "", threshold, err
	}
	if stat.Size() == 0 {
		return "", "", threshold, errors.New("empty fixture")
	}
	db, conns, err := openPool(o.db, "mvcc", o.conns)
	if err != nil {
		return "", "", threshold, err
	}
	defer func() { runErr = errors.Join(runErr, closePool(db, conns)) }()
	ctx := context.Background()
	var readbacks []string
	for i, c := range conns {
		if _, err := c.ExecContext(ctx, fmt.Sprintf("PRAGMA cache_size = -%d", o.cacheKiB)); err != nil {
			return "", "", threshold, err
		}
		var size int64
		if err := c.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&size); err != nil {
			return "", "", threshold, err
		}
		readbacks = append(readbacks, fmt.Sprintf("conn_%d_cache_size=%d", i+1, size))
	}
	baseline, err := r.sample("baseline", 0, 0, 0, strings.Join(readbacks, " "))
	if err != nil {
		return "", "", threshold, err
	}
	previous := baseline
	var deltas, totals []string
	var last measurement
	for i, c := range conns {
		var bodyBytes, docs int64
		if err := c.QueryRowContext(ctx, "SELECT sum(length(body)) FROM docs").Scan(&bodyBytes); err != nil {
			return "", "", threshold, err
		}
		if err := c.QueryRowContext(ctx, "SELECT count(*) FROM docs WHERE lookup >= ''").Scan(&docs); err != nil {
			return "", "", threshold, err
		}
		last, err = r.sample("after_read", docs, 0, i+1, fmt.Sprintf("%s body_bytes=%d indexed_docs=%d", readbacks[i], bodyBytes, docs))
		if err != nil {
			return "", "", threshold, err
		}
		deltas = append(deltas, fmt.Sprintf("%.3f", float64(last.rss-previous.rss)/1024))
		totals = append(totals, fmt.Sprintf("%.3f", float64(last.rss-baseline.rss)/1024))
		previous = last
	}
	delta := float64(last.rss-baseline.rss) / 1024
	bound := float64(o.conns)*float64(o.cacheKiB)/1024 + 64
	return reproduced(delta > bound), fmt.Sprintf("delta_mib=%.3f bound_mib=%.3f delta_db_ratio=%.3f per_conn_delta_mib=%s cumulative_delta_mib=%s %s", delta, bound, delta*1024*1024/float64(stat.Size()), strings.Join(deltas, ","), strings.Join(totals, ","), strings.Join(readbacks, " ")), threshold, nil
}
