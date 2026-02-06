// Package repository provides generic CRUD operations for database entities.
// Uses struct tags for automatic row scanning.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	"github.com/fightbulc/go-turso-kit/pkg/scan"
)

var (
	// ErrNotFound is returned when an entity is not found
	ErrNotFound = errors.New("repository: entity not found")
	// ErrNilDB is returned when database is nil
	ErrNilDB = errors.New("repository: database cannot be nil")
	// ErrEmptyTableName is returned when table name is empty
	ErrEmptyTableName = errors.New("repository: table name cannot be empty")
)

// Repository provides generic CRUD operations for entities.
// Entities must use `db` struct tags for column mapping.
//
// Example:
//
//	type User struct {
//	    ID    string `db:"id"`
//	    Email string `db:"email"`
//	    Name  string `db:"name"`
//	}
//
//	repo := repository.New[User, string](db, "users")
//	user, err := repo.FindByID(ctx, "user_123")
type Repository[T any, ID comparable] struct {
	db        *sql.DB
	tableName string
}

// New creates a new Repository instance.
// T is the entity type (must have `db` struct tags).
// ID is the primary key type.
func New[T any, ID comparable](db *sql.DB, tableName string) *Repository[T, ID] {
	return &Repository[T, ID]{
		db:        db,
		tableName: tableName,
	}
}

// DB returns the underlying database connection.
func (r *Repository[T, ID]) DB() *sql.DB {
	return r.db
}

// TableName returns the table name.
func (r *Repository[T, ID]) TableName() string {
	return r.tableName
}

// FindByID retrieves an entity by its primary key.
// Returns ErrNotFound if no matching row exists.
func (r *Repository[T, ID]) FindByID(ctx context.Context, id ID) (T, error) {
	var zero T

	if r.db == nil {
		return zero, ErrNilDB
	}

	q, err := query.Build(
		fmt.Sprintf("SELECT * FROM %s WHERE id = :id LIMIT 1", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return zero, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return zero, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	entity, err := scan.Row[T](rows)
	if errors.Is(err, sql.ErrNoRows) {
		return zero, ErrNotFound
	}
	if err != nil {
		return zero, fmt.Errorf("failed to scan row: %w", err)
	}

	return entity, nil
}

// FindAll retrieves all entities from the table.
// Returns empty slice (not nil) if no rows.
func (r *Repository[T, ID]) FindAll(ctx context.Context) ([]T, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}

	q, err := query.New("SELECT * FROM " + r.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, q.SQL())
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.All[T](rows)
}

// FindByQuery retrieves entities matching a custom query.
func (r *Repository[T, ID]) FindByQuery(ctx context.Context, q *query.Query) ([]T, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	rows, err := r.db.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.All[T](rows)
}

// FindOneByQuery retrieves a single entity matching a query.
// Returns nil (not error) if no rows found.
func (r *Repository[T, ID]) FindOneByQuery(ctx context.Context, q *query.Query) (*T, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	rows, err := r.db.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.One[T](rows)
}

// Count returns the total number of entities in the table.
func (r *Repository[T, ID]) Count(ctx context.Context) (int64, error) {
	if r.db == nil {
		return 0, ErrNilDB
	}

	sqlStr := "SELECT COUNT(*) FROM " + r.tableName
	var count int64
	err := r.db.QueryRowContext(ctx, sqlStr).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}

	return count, nil
}

// Exists checks if an entity with the given ID exists.
func (r *Repository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	if r.db == nil {
		return false, ErrNilDB
	}

	q, err := query.Build(
		fmt.Sprintf("SELECT 1 FROM %s WHERE id = :id LIMIT 1", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return false, fmt.Errorf("failed to build query: %w", err)
	}

	var exists int
	err = r.db.QueryRowContext(ctx, q.SQL(), q.Args()...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

// Insert executes an INSERT query.
func (r *Repository[T, ID]) Insert(ctx context.Context, q *query.Query) (sql.Result, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to insert: %w", err)
	}

	return result, nil
}

// Update executes an UPDATE query.
func (r *Repository[T, ID]) Update(ctx context.Context, q *query.Query) (sql.Result, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to update: %w", err)
	}

	return result, nil
}

// Delete executes a DELETE query.
func (r *Repository[T, ID]) Delete(ctx context.Context, q *query.Query) (sql.Result, error) {
	if r.db == nil {
		return nil, ErrNilDB
	}
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to delete: %w", err)
	}

	return result, nil
}

// DeleteByID deletes an entity by its primary key.
// Returns ErrNotFound if no matching row exists.
func (r *Repository[T, ID]) DeleteByID(ctx context.Context, id ID) error {
	if r.db == nil {
		return ErrNilDB
	}

	q, err := query.Build(
		fmt.Sprintf("DELETE FROM %s WHERE id = :id", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	result, err := r.db.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return fmt.Errorf("failed to delete: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrNotFound
	}

	return nil
}
