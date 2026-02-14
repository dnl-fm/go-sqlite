package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// WithTx executes a function within a transaction.
// The underlying DBTX must be a *sql.DB (not already a *sql.Tx).
// If the function returns an error, the transaction is rolled back.
// Otherwise, the transaction is committed.
//
// Example:
//
//	err := repo.WithTx(ctx, func(txRepo *Repository[User, string]) error {
//	    q, _ := query.Build("INSERT INTO users ...", params)
//	    txRepo.Insert(ctx, q)
//	    return nil // commit
//	})
func (r *Repository[T, ID]) WithTx(ctx context.Context, fn func(*Repository[T, ID]) error) error {
	if r.db == nil {
		return ErrNilDB
	}

	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("WithTx requires *sql.DB, got %T (already in a transaction?)", r.db)
	}

	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	txRepo := &Repository[T, ID]{
		db:        tx,
		tableName: r.tableName,
		tableSQL:  r.tableSQL,
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
