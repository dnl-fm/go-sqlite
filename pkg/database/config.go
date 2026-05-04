package database

import (
	"time"

	"github.com/dnl-fm/go-sqlite/pkg/driver/turso"
)

// DefaultDriver is the driver used when none is specified.
const DefaultDriver = "sqlite"

// Config holds database connection pool and pragma settings
type Config struct {
	Pragmas         map[string]string
	Driver          string
	ConnMaxLifetime time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
}

// DefaultConfig returns a configuration suitable for general use
func DefaultConfig() *Config {
	return &Config{
		Driver:          DefaultDriver,
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		Pragmas: map[string]string{
			"journal_mode": "WAL",
			"synchronous":  "NORMAL",
			"foreign_keys": "ON",
			"busy_timeout": "5000",
			"temp_store":   "MEMORY",
			"cache_size":   "-20000",   // 20MB
			"mmap_size":    "33554432", // 32MB
		},
	}
}

// DevelopmentConfig returns a configuration optimized for development
func DevelopmentConfig() *Config {
	return &Config{
		Driver:          DefaultDriver,
		MaxOpenConns:    10,
		MaxIdleConns:    2,
		ConnMaxLifetime: 1 * time.Minute,
		Pragmas: map[string]string{
			"journal_mode": "DELETE",
			"synchronous":  "FULL",
			"cache_size":   "-5000", // 5MB
			"busy_timeout": "3000",
		},
	}
}

// ProductionConfig returns a configuration optimized for production
func ProductionConfig() *Config {
	return &Config{
		Driver:          DefaultDriver,
		MaxOpenConns:    100,
		MaxIdleConns:    10,
		ConnMaxLifetime: 15 * time.Minute,
		Pragmas: map[string]string{
			"journal_mode": "WAL",
			"synchronous":  "NORMAL",
			"foreign_keys": "ON",
			"busy_timeout": "10000",
			"temp_store":   "MEMORY",
			"cache_size":   "-64000",   // 64MB
			"mmap_size":    "67108864", // 64MB
		},
	}
}

// TursoMVCCConfig returns a Turso configuration for concurrent write workloads.
//
// MVCC mode allows BEGIN CONCURRENT transactions to overlap across connections
// and processes writing to the same database file.
func TursoMVCCConfig() *Config {
	return ProductionConfig().
		WithDriver(turso.DriverName).
		WithPragma("journal_mode", "'mvcc'").
		WithPragma("busy_timeout", "1000")
}

// WithTursoMVCC configures the database to use Turso with MVCC journal mode.
func WithTursoMVCC() Option {
	return WithConfig(TursoMVCCConfig())
}

// WithDriver sets the database/sql driver name.
// Returns the config unchanged if name is empty.
func (c *Config) WithDriver(name string) *Config {
	if name != "" {
		c.Driver = name
	}
	return c
}

// WithMaxOpenConns sets the maximum number of open connections
func (c *Config) WithMaxOpenConns(n int) *Config {
	c.MaxOpenConns = n
	return c
}

// WithMaxIdleConns sets the maximum number of idle connections
func (c *Config) WithMaxIdleConns(n int) *Config {
	c.MaxIdleConns = n
	return c
}

// WithConnMaxLifetime sets the maximum lifetime for a connection
func (c *Config) WithConnMaxLifetime(d time.Duration) *Config {
	c.ConnMaxLifetime = d
	return c
}

// WithPragma adds or updates a pragma setting
func (c *Config) WithPragma(key, value string) *Config {
	if c.Pragmas == nil {
		c.Pragmas = make(map[string]string)
	}
	c.Pragmas[key] = value
	return c
}
