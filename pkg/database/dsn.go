package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func databaseTarget(path string) (*url.URL, bool, error) {
	var target *url.URL
	if strings.HasPrefix(path, "file:") {
		var err error
		target, err = url.Parse(path)
		if err != nil {
			return nil, false, err
		}
		if target.Opaque != "" {
			target.Path, target.Opaque = target.Opaque, ""
		}
	} else {
		target = &url.URL{Scheme: "file", Path: path}
	}
	memory := target.Path == memoryPath || target.Query().Get("mode") == "memory"
	if memory {
		target.Opaque, target.Path = target.Path, ""
	}
	if !memory {
		absolute, err := filepath.Abs(target.Path)
		if err != nil {
			return nil, false, err
		}
		target.Path = absolute
	}
	return target, memory, nil
}

// rejectLegacyFile runs before SQLite touches a file in an incompatible format.
func rejectLegacyFile(path string) error {
	if _, err := os.Stat(path + "-log"); err == nil {
		return fmt.Errorf("turso MVCC database %q: convert with cmd/turso-convert before opening with v0.8.0", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	header := make([]byte, 20)
	n, readErr := io.ReadFull(file, header)
	closeErr := file.Close()
	if n >= 20 && header[18] == 0xff && header[19] == 0xff {
		return fmt.Errorf("turso MVCC database %q: convert with cmd/turso-convert before opening with v0.8.0", path)
	}
	if errors.Is(readErr, io.EOF) || errors.Is(readErr, io.ErrUnexpectedEOF) {
		readErr = nil
	}
	return errors.Join(readErr, closeErr)
}

func orderedPragmas(cfg *Config) []string {
	keys := make([]string, 0, len(cfg.Pragmas))
	seen := make(map[string]bool, len(cfg.Pragmas))
	for _, key := range cfg.PragmaOrder {
		if _, ok := cfg.Pragmas[key]; ok && !seen[key] {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	remaining := make([]string, 0, len(cfg.Pragmas)-len(keys))
	for key := range cfg.Pragmas {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	return append(keys, remaining...)
}

// Check on the target before encoding: non-strict mode omits rejected settings.
// Modernc reapplies the accepted list on every pooled connection via its DSN.
func checkedPragmas(ctx context.Context, target *url.URL, cfg *Config) ([]string, error) {
	probe, err := sql.Open("sqlite", connectionDSN(target, nil, false))
	if err != nil {
		return nil, err
	}
	probe.SetMaxOpenConns(1)
	pragmas, validationErr := validatePragmas(ctx, probe, cfg)
	return pragmas, errors.Join(validationErr, probe.Close())
}

func validatePragmas(ctx context.Context, probe *sql.DB, cfg *Config) ([]string, error) {
	pragmas := make([]string, 0, len(cfg.Pragmas))
	for _, key := range orderedPragmas(cfg) {
		setting := key + " = " + cfg.Pragmas[key]
		// DSN pragmas cannot contain several statements.
		if strings.Contains(setting, ";") {
			if cfg.StrictPragmas {
				return nil, fmt.Errorf("invalid PRAGMA %s: multiple statements", setting)
			}
			continue
		}
		if _, execErr := probe.ExecContext(ctx, "PRAGMA "+setting); execErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if cfg.StrictPragmas {
				return nil, fmt.Errorf("set PRAGMA %s: %w", setting, execErr)
			}
			continue
		}
		pragmas = append(pragmas, setting)
	}
	return pragmas, nil
}

func connectionDSN(target *url.URL, pragmas []string, readOnly bool) string {
	dsn := *target
	params := dsn.Query()
	params.Del("_pragma")
	params.Del("_txlock")
	for _, pragma := range pragmas {
		params.Add("_pragma", pragma)
	}
	if readOnly {
		params.Set("mode", "ro")
	} else {
		if params.Get("mode") != "memory" {
			params.Set("mode", "rwc")
		}
		params.Set("_txlock", "immediate")
	}
	dsn.RawQuery = params.Encode()
	return dsn.String()
}
