package database

import "time"

const (
	walJournalMode = "WAL"
	busyTimeoutKey = "busy_timeout"
	memoryPath     = ":memory:"
	cacheSizeKey   = "cache_size"
	journalModeKey = "journal_mode"
	synchronousKey = "synchronous"
	foreignKeysKey = "foreign_keys"
	tempStoreKey   = "temp_store"
)

// Config holds reader-pool settings and PRAGMAs shared by both pools.
// The writer always has one permanent connection. In-memory databases use
// one permanent connection for both roles, ignoring these pool settings.
type Config struct {
	Pragmas         map[string]string
	PragmaOrder     []string
	StrictPragmas   bool
	ConnMaxLifetime time.Duration
	MaxOpenConns    int
	MaxIdleConns    int
}

// DefaultConfig returns the standard SQLite WAL configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxOpenConns: 4, MaxIdleConns: 4,
		PragmaOrder: []string{journalModeKey, synchronousKey, busyTimeoutKey, foreignKeysKey, tempStoreKey, cacheSizeKey},
		Pragmas: map[string]string{
			journalModeKey: walJournalMode, synchronousKey: "NORMAL", busyTimeoutKey: "5000",
			foreignKeysKey: "ON", tempStoreKey: "MEMORY", cacheSizeKey: "-16000",
		},
	}
}

// DevelopmentConfig returns the standard SQLite WAL configuration.
func DevelopmentConfig() *Config { return DefaultConfig() }

// ProductionConfig returns the standard SQLite WAL configuration.
func ProductionConfig() *Config { return DefaultConfig() }

// WithMaxOpenConns sets the maximum number of open reader connections
func (c *Config) WithMaxOpenConns(n int) *Config {
	c.MaxOpenConns = n
	return c
}

// WithMaxIdleConns sets the maximum number of idle reader connections
func (c *Config) WithMaxIdleConns(n int) *Config {
	c.MaxIdleConns = n
	return c
}

// WithConnMaxLifetime sets the maximum lifetime for reader connections
func (c *Config) WithConnMaxLifetime(d time.Duration) *Config {
	c.ConnMaxLifetime = d
	return c
}

// WithPragma adds or updates a pragma setting
func (c *Config) WithPragma(key, value string) *Config {
	if c.Pragmas == nil {
		c.Pragmas = make(map[string]string)
	}
	if _, exists := c.Pragmas[key]; !exists {
		c.PragmaOrder = append(c.PragmaOrder, key)
	}
	c.Pragmas[key] = value
	return c
}

// WithPragmaOrder sets the preferred pragma application order.
func (c *Config) WithPragmaOrder(keys ...string) *Config {
	c.PragmaOrder = append([]string(nil), keys...)
	return c
}

// WithStrictPragmas makes Open fail if any configured PRAGMA cannot be applied.
func (c *Config) WithStrictPragmas(strict bool) *Config {
	c.StrictPragmas = strict
	return c
}
