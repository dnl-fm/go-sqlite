// Package turso registers the Turso database/sql driver and provides its driver name.
//
// Import as a blank import to register the driver:
//
//	import _ "github.com/dnl-fm/go-sqlite/pkg/driver/turso"
//
// Then use with database.Open:
//
//	db, err := database.Open(ctx, "data.db", database.WithDriver(turso.DriverName))
//
// To use concurrent write transactions, enable MVCC with:
//
//	PRAGMA journal_mode = 'mvcc'
//
// and start write transactions with:
//
//	BEGIN CONCURRENT
//
// Turso MVCC does not support writes to WITHOUT ROWID tables. Databases opened
// in MVCC mode must use normal rowid tables.
package turso

import _ "turso.tech/database/tursogo" // registers "turso" driver with database/sql

// DriverName is the name used with database.WithDriver.
const DriverName = "turso"
