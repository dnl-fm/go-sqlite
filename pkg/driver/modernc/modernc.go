// Package modernc registers the modernc.org/sqlite driver and provides its driver name.
//
// Import as blank import to register the driver:
//
//	import _ "github.com/dnl-fm/go-sqlite/pkg/driver/modernc"
//
// Then use with database.Open:
//
//	db, err := database.Open(ctx, "data.db", database.WithDriver(modernc.DriverName))
package modernc

import _ "modernc.org/sqlite" // registers "sqlite" driver with database/sql

// DriverName is the name used with database.WithDriver.
const DriverName = "sqlite"
