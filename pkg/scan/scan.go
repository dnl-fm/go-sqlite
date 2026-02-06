// Package scan provides struct scanning from sql.Rows using db struct tags.
// It eliminates manual row scanning and column order dependencies.
package scan

import (
	"database/sql"
	"errors"
	"reflect"
	"sync"
)

var (
	// ErrNotPointer is returned when destination is not a pointer
	ErrNotPointer = errors.New("scan: destination must be a pointer")
	// ErrNotStruct is returned when destination is not a struct
	ErrNotStruct = errors.New("scan: destination must be a struct")
)

// fieldInfo caches struct field information
type fieldInfo struct {
	name  string
	index int
}

// structCache caches struct field mappings to avoid repeated reflection
var structCache sync.Map // map[reflect.Type]map[string]fieldInfo

// Row scans a single row into a struct using `db` tags.
// Returns sql.ErrNoRows if no rows available.
func Row[T any](rows *sql.Rows) (T, error) {
	var result T

	if !rows.Next() {
		err := rows.Err()
		if err != nil {
			return result, err
		}
		return result, sql.ErrNoRows
	}

	err := scanStruct(rows, &result)
	if err != nil {
		return result, err
	}

	return result, nil
}

// All scans all rows into a slice of structs using `db` tags.
// Returns empty slice (not nil) if no rows.
func All[T any](rows *sql.Rows) ([]T, error) {
	results := make([]T, 0)

	for rows.Next() {
		var item T
		err := scanStruct(rows, &item)
		if err != nil {
			return nil, err
		}
		results = append(results, item)
	}

	err := rows.Err()
	if err != nil {
		return nil, err
	}

	return results, nil
}

// One scans a single row, returning a pointer (nil if no rows).
// Unlike Row, does not return error for no rows.
func One[T any](rows *sql.Rows) (*T, error) {
	if !rows.Next() {
		err := rows.Err()
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	var result T
	err := scanStruct(rows, &result)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// scanStruct scans the current row into a struct pointer
func scanStruct(rows *sql.Rows, dest any) error {
	v := reflect.ValueOf(dest)
	if v.Kind() != reflect.Ptr {
		return ErrNotPointer
	}

	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return ErrNotStruct
	}

	columns, err := rows.Columns()
	if err != nil {
		return err
	}

	// Get or build field map
	fieldMap := getFieldMap(v.Type())

	// Create scan destinations
	scanDest := make([]any, len(columns))
	for i, col := range columns {
		if fi, ok := fieldMap[col]; ok {
			scanDest[i] = v.Field(fi.index).Addr().Interface()
		} else {
			// Column not mapped to struct field - discard
			scanDest[i] = new(any)
		}
	}

	return rows.Scan(scanDest...)
}

// getFieldMap returns cached field mapping for a struct type
func getFieldMap(t reflect.Type) map[string]fieldInfo {
	if cached, ok := structCache.Load(t); ok {
		if fm, assertOk := cached.(map[string]fieldInfo); assertOk {
			return fm
		}
	}

	fieldMap := buildFieldMap(t)
	structCache.Store(t, fieldMap)
	return fieldMap
}

// buildFieldMap creates a column name -> field info mapping
func buildFieldMap(t reflect.Type) map[string]fieldInfo {
	fieldMap := make(map[string]fieldInfo)

	for i := range t.NumField() {
		field := t.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Get db tag
		tag := field.Tag.Get("db")
		if tag == "" || tag == "-" {
			continue
		}

		fieldMap[tag] = fieldInfo{
			index: i,
			name:  field.Name,
		}
	}

	return fieldMap
}
