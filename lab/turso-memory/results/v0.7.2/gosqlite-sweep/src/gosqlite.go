package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dnl-fm/go-sqlite/pkg/database"
)

func goSQLiteConfig(o options) (*database.Config, error) {
	if o.mvccGC < -1 || o.mvccGC == 0 || o.mvccCheckpoint < -1 {
		return nil, errors.New("GC threshold must be positive or -1 (unset); checkpoint threshold must be nonnegative or -1 (unset)")
	}
	cfg := database.DefaultConfig()
	if o.mvccGC != -1 {
		cfg.WithPragma("mvcc_gc_threshold", strconv.FormatInt(o.mvccGC, 10))
	}
	if o.mvccCheckpoint != -1 {
		cfg.WithPragma("mvcc_checkpoint_threshold", strconv.FormatInt(o.mvccCheckpoint, 10))
	}
	return cfg, nil
}

func openGoSQLitePool(o options, r *recorder) (*sql.DB, []*sql.Conn, string, error) {
	cfg, err := goSQLiteConfig(o)
	if err != nil {
		return nil, nil, "", err
	}
	ctx := context.Background()
	wrapped, err := database.Open(ctx, o.db, database.WithConfig(cfg))
	if err != nil {
		return nil, nil, "", err
	}
	db := wrapped.DB()
	var conns []*sql.Conn
	var readbacks []string
	for i := 0; i < cfg.MaxOpenConns; i++ {
		c, err := db.Conn(ctx)
		if err == nil {
			conns = append(conns, c)
			var warm int
			err = c.QueryRowContext(ctx, "SELECT 1").Scan(&warm)
		}
		var detail string
		if err == nil {
			detail, err = goSQLiteReadbacks(ctx, c, cfg, i+1)
		}
		if err == nil {
			_, err = r.sample("pragma_readback", 0, 0, i+1, detail)
		}
		if err != nil {
			return nil, nil, "", errors.Join(fmt.Errorf("go-sqlite connection %d: %w", i+1, err), closePool(db, conns))
		}
		readbacks = append(readbacks, detail)
	}
	arena := os.Getenv("MALLOC_ARENA_MAX")
	if arena == "" {
		arena = "unset"
	}
	readbacks = append(readbacks, "malloc_arena_max="+arena)
	return db, conns, strings.Join(readbacks, " "), nil
}

func goSQLiteReadbacks(ctx context.Context, c *sql.Conn, cfg *database.Config, number int) (string, error) {
	var readbacks []string
	for _, name := range []string{"foreign_keys", "busy_timeout", "cache_size", "mvcc_gc_threshold", "mvcc_checkpoint_threshold"} {
		var actual int64
		if err := c.QueryRowContext(ctx, "PRAGMA "+name).Scan(&actual); err != nil {
			return "", fmt.Errorf("readback %s: %w", name, err)
		}
		if configured, ok := cfg.Pragmas[name]; ok {
			want, err := strconv.ParseInt(configured, 10, 64)
			if name == "foreign_keys" && strings.EqualFold(configured, "ON") {
				want, err = 1, nil
			}
			if err != nil {
				return "", err
			}
			if actual != want {
				return "", fmt.Errorf("readback %s=%d, want %d", name, actual, want)
			}
		}
		readbacks = append(readbacks, fmt.Sprintf("conn%d_%s=%d", number, name, actual))
	}
	return strings.Join(readbacks, " "), nil
}
