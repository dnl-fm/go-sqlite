package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	"github.com/fightbulc/go-turso-kit/pkg/scan"
)

// TxRepository wraps a Repository and executes operations within a transaction.
type TxRepository[T any, ID comparable] struct {
	tx        *sql.Tx
	tableName string
}

// WithTx executes a function within a transaction.
// If the function returns an error, the transaction is rolled back.
// Otherwise, the transaction is committed.
//
// Example:
//
//	err := repo.WithTx(ctx, func(tx *TxRepository[User, string]) error {
//	    // All operations here are in the same transaction
//	    q, _ := query.Build("INSERT INTO users ...", params)
//	    tx.Insert(ctx, q)
//	    return nil // commit
//	})
func (r *Repository[T, ID]) WithTx(ctx context.Context, fn func(*TxRepository[T, ID]) error) error {
	if r.db == nil {
		return ErrNilDB
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	txRepo := &TxRepository[T, ID]{
		tx:        tx,
		tableName: r.tableName,
	}

	err = fn(txRepo)
	if err != nil {
		rbErr := tx.Rollback()
		if rbErr != nil {
			return fmt.Errorf("rollback failed: %w (original error: %w)", rbErr, err)
		}
		return err
	}

	commitErr := tx.Commit()
	if commitErr != nil {
		return fmt.Errorf("failed to commit transaction: %w", commitErr)
	}

	return nil
}

// Tx returns the underlying transaction.
func (r *TxRepository[T, ID]) Tx() *sql.Tx {
	return r.tx
}

// FindByID retrieves an entity by its primary key within the transaction.
func (r *TxRepository[T, ID]) FindByID(ctx context.Context, id ID) (T, error) {
	var zero T

	q, err := query.Build(
		fmt.Sprintf("SELECT * FROM %s WHERE id = :id LIMIT 1", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return zero, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := r.tx.QueryContext(ctx, q.SQL(), q.Args()...)
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

// FindAll retrieves all entities from the table within the transaction.
func (r *TxRepository[T, ID]) FindAll(ctx context.Context) ([]T, error) {
	q, err := query.New("SELECT * FROM " + r.tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := r.tx.QueryContext(ctx, q.SQL())
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.All[T](rows)
}

// FindByQuery retrieves entities matching a custom query within the transaction.
func (r *TxRepository[T, ID]) FindByQuery(ctx context.Context, q *query.Query) ([]T, error) {
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	rows, err := r.tx.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.All[T](rows)
}

// FindOneByQuery retrieves a single entity matching a query within the transaction.
func (r *TxRepository[T, ID]) FindOneByQuery(ctx context.Context, q *query.Query) (*T, error) {
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	rows, err := r.tx.QueryContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to query: %w", err)
	}
	defer rows.Close()

	return scan.One[T](rows)
}

// Count returns the total number of entities in the table within the transaction.
func (r *TxRepository[T, ID]) Count(ctx context.Context) (int64, error) {
	sqlStr := "SELECT COUNT(*) FROM " + r.tableName
	var count int64
	err := r.tx.QueryRowContext(ctx, sqlStr).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count: %w", err)
	}
	return count, nil
}

// Exists checks if an entity with the given ID exists within the transaction.
func (r *TxRepository[T, ID]) Exists(ctx context.Context, id ID) (bool, error) {
	q, err := query.Build(
		fmt.Sprintf("SELECT 1 FROM %s WHERE id = :id LIMIT 1", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return false, fmt.Errorf("failed to build query: %w", err)
	}

	var exists int
	err = r.tx.QueryRowContext(ctx, q.SQL(), q.Args()...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to check existence: %w", err)
	}

	return true, nil
}

// Insert executes an INSERT query within the transaction.
func (r *TxRepository[T, ID]) Insert(ctx context.Context, q *query.Query) (sql.Result, error) {
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.tx.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to insert: %w", err)
	}

	return result, nil
}

// Update executes an UPDATE query within the transaction.
func (r *TxRepository[T, ID]) Update(ctx context.Context, q *query.Query) (sql.Result, error) {
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.tx.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to update: %w", err)
	}

	return result, nil
}

// Delete executes a DELETE query within the transaction.
func (r *TxRepository[T, ID]) Delete(ctx context.Context, q *query.Query) (sql.Result, error) {
	if q == nil {
		return nil, errors.New("query cannot be nil")
	}

	result, err := r.tx.ExecContext(ctx, q.SQL(), q.Args()...)
	if err != nil {
		return nil, fmt.Errorf("failed to delete: %w", err)
	}

	return result, nil
}

// DeleteByID deletes an entity by its primary key within the transaction.
func (r *TxRepository[T, ID]) DeleteByID(ctx context.Context, id ID) error {
	q, err := query.Build(
		fmt.Sprintf("DELETE FROM %s WHERE id = :id", r.tableName),
		map[string]any{"id": id},
	)
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	result, err := r.tx.ExecContext(ctx, q.SQL(), q.Args()...)
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
