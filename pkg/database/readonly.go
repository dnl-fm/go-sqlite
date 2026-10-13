package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"strings"
)

// OpenReadOnly opens an existing WAL database for a process that does not own
// it. The handle has a reader pool only: every connection uses mode=ro, so
// SQLite refuses writes below the SQL layer. Exec, Write, BeginTx without
// ReadOnly, Checkpoint and Queue.Submit return ErrReadOnly.
//
// It never creates the file, and it refuses Turso-format files like Open.
// The owner sets journal_mode and synchronous, so those two are never sent.
// Every other configured PRAGMA is tried on a mode=ro connection first:
// SQLite rejects the ones that would write the file, and they are omitted
// (or fail the open with StrictPragmas).
func OpenReadOnly(ctx context.Context, path string, opts ...Option) (*Database, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	d := &Database{path: path, config: DefaultConfig(), readOnly: true}
	for _, opt := range opts {
		if err := opt(d); err != nil {
			return nil, err
		}
	}
	target, err := readOnlyTarget(path)
	if err != nil {
		return nil, err
	}
	pragmas, err := checkedPragmas(ctx, target, readOnlyConfig(d.config), true)
	if err != nil {
		return nil, err
	}
	d.reader, err = sql.Open("sqlite", connectionDSN(target, pragmas, true))
	if err != nil {
		return nil, err
	}
	readers := d.config.MaxOpenConns
	if readers <= 0 {
		readers = 4
	}
	d.reader.SetMaxOpenConns(readers)
	d.reader.SetMaxIdleConns(d.config.MaxIdleConns)
	d.reader.SetConnMaxLifetime(d.config.ConnMaxLifetime)
	if walErr := requireWAL(ctx, d.reader, target.Path); walErr != nil {
		return nil, errors.Join(walErr, d.reader.Close())
	}
	d.writer = d.reader
	return d, nil
}

// requireWAL fails on a rollback-journal file, which only its owner can switch.
func requireWAL(ctx context.Context, reader *sql.DB, path string) error {
	var mode string
	if err := reader.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, walJournalMode) {
		return fmt.Errorf("database %q has journal_mode %q, want %s: a read-only connection cannot switch it, and readers of a rollback-journal file block the owner's writes", path, mode, walJournalMode)
	}
	return nil
}

// readOnlyTarget runs the checks that must pass before SQLite touches the file.
func readOnlyTarget(path string) (*url.URL, error) {
	target, memory, err := databaseTarget(path)
	if err != nil {
		return nil, err
	}
	if memory {
		return nil, fmt.Errorf("%w: read-only open of in-memory path %q would be a separate empty database", ErrInvalidPath, path)
	}
	if _, statErr := os.Stat(target.Path); statErr != nil {
		return nil, fmt.Errorf("open %q read-only (its owner creates it): %w", target.Path, statErr)
	}
	return target, rejectLegacyFile(target.Path)
}

// readOnlyConfig drops the PRAGMAs that belong to the file's owner.
func readOnlyConfig(cfg *Config) *Config {
	filtered := *cfg
	filtered.Pragmas = maps.Clone(cfg.Pragmas)
	maps.DeleteFunc(filtered.Pragmas, func(key, _ string) bool {
		return strings.EqualFold(key, journalModeKey) || strings.EqualFold(key, synchronousKey)
	})
	return &filtered
}
