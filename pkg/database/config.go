package database

import "time"

// Config holds database connection pool and pragma settings
type Config struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	Pragmas         map[string]string
}

// DefaultConfig returns a configuration suitable for general use
func DefaultConfig() *Config {
	return &Config{
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		Pragmas: map[string]string{
			"journal_mode": "WAL",
			"synchronous":  "NORMAL",
			"cache_size":   "-20000", // 20MB
			"busy_timeout": "5000",
		},
	}
}

// DevelopmentConfig returns a configuration optimized for development
func DevelopmentConfig() *Config {
	return &Config{
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
		MaxOpenConns:    100,
		MaxIdleConns:    10,
		ConnMaxLifetime: 15 * time.Minute,
		Pragmas: map[string]string{
			"journal_mode": "WAL",
			"synchronous":  "NORMAL",
			"cache_size":   "-64000", // 64MB
			"busy_timeout": "10000",
			"foreign_keys": "ON",
		},
	}
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
