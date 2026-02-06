// Package sqlite3 provides the driver name constant for github.com/mattn/go-sqlite3.
//
// Unlike pkg/driver/modernc, this package does NOT register the driver via blank import
// because the sqlite3 dependency (which requires CGo) is not included by default. To use:
//
//  1. Add to go.mod: require github.com/mattn/go-sqlite3 v1.14.32
//  2. Register in your main package: import _ "github.com/mattn/go-sqlite3"
//  3. Use with database.Open: database.WithDriver(sqlite3.DriverName)
package sqlite3

// DriverName is the name used with database.WithDriver.
const DriverName = "sqlite3"
