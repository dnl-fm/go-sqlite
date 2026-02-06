// Package turso provides the driver name constant for turso.tech/database/tursogo.
//
// Unlike pkg/driver/modernc, this package does NOT register the driver via blank import
// because the turso dependency is not included by default. To use this driver:
//
//  1. Add to go.mod: require turso.tech/database/tursogo v0.4.4
//  2. Register in your main package: import _ "turso.tech/database/tursogo"
//  3. Use with database.Open: database.WithDriver(turso.DriverName)
package turso

// DriverName is the name used with database.WithDriver.
const DriverName = "turso"
