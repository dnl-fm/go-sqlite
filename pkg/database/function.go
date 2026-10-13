package database

import (
	"database/sql/driver"

	"modernc.org/sqlite"
)

// Custom SQL functions use the driver's own types, so consumers need no
// modernc import.
type (
	// FunctionContext is the context a custom SQL function executes in.
	FunctionContext = sqlite.FunctionContext
	// FunctionImpl describes a scalar or aggregate function for RegisterFunction.
	FunctionImpl = sqlite.FunctionImpl
	// AggregateFunction is what FunctionImpl.MakeAggregate returns.
	AggregateFunction = sqlite.AggregateFunction
)

// RegisterFunction registers a custom SQL function described by impl.
//
// Registration is process-global: every connection opened afterwards has the
// function, on the writer and on the reader pool, in every Database. Register
// before Open or OpenReadOnly; connections that are already open never get it.
// A name can be registered once.
func RegisterFunction(name string, impl *FunctionImpl) error {
	return sqlite.RegisterFunction(name, impl)
}

// RegisterScalarFunction registers a scalar function with nArg arguments
// (-1 for variadic). See RegisterFunction for when and where it applies.
func RegisterScalarFunction(
	name string,
	nArg int32,
	fn func(ctx *FunctionContext, args []driver.Value) (driver.Value, error),
) error {
	return sqlite.RegisterScalarFunction(name, nArg, fn)
}

// RegisterDeterministicScalarFunction registers a scalar function that always
// returns the same output for the same arguments. SQLite allows only such
// functions in index expressions and generated columns. See RegisterFunction
// for when and where it applies.
func RegisterDeterministicScalarFunction(
	name string,
	nArg int32,
	fn func(ctx *FunctionContext, args []driver.Value) (driver.Value, error),
) error {
	return sqlite.RegisterDeterministicScalarFunction(name, nArg, fn)
}
