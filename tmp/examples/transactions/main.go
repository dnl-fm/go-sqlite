package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/tursodatabase/turso-go"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	"github.com/fightbulc/go-turso-kit/pkg/repository"
)

// Account represents a bank account
type Account struct {
	ID      int     `db:"id"`
	Name    string  `db:"name"`
	Balance float64 `db:"balance"`
}

func main() {
	ctx := context.Background()

	// Open in-memory database
	db, err := sql.Open("turso", ":memory:")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create accounts table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE accounts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			balance REAL NOT NULL DEFAULT 0
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Create repository
	accountRepo := repository.New[Account, int](db, "accounts")

	// Insert initial accounts
	fmt.Println("=== Setting up accounts ===")
	_, err = db.ExecContext(ctx, "INSERT INTO accounts (name, balance) VALUES ('Alice', 1000)")
	if err != nil {
		log.Fatalf("Failed to insert: %v", err)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO accounts (name, balance) VALUES ('Bob', 500)")
	if err != nil {
		log.Fatalf("Failed to insert: %v", err)
	}

	printBalances(ctx, accountRepo)

	// Successful transaction: transfer money
	fmt.Println("\n=== Successful Transfer (Alice -> Bob, $200) ===")
	err = transfer(ctx, accountRepo, 1, 2, 200)
	if err != nil {
		log.Printf("Transfer failed: %v", err)
	} else {
		fmt.Println("✓ Transfer completed")
	}
	printBalances(ctx, accountRepo)

	// Failed transaction: insufficient funds
	fmt.Println("\n=== Failed Transfer (Alice -> Bob, $1000 - insufficient) ===")
	err = transfer(ctx, accountRepo, 1, 2, 1000)
	if err != nil {
		fmt.Printf("✗ Transfer failed (expected): %v\n", err)
	} else {
		fmt.Println("Transfer unexpectedly succeeded")
	}
	printBalances(ctx, accountRepo)

	fmt.Println("\n✅ Transaction example complete!")
}

func transfer(ctx context.Context, repo *repository.Repository[Account, int], fromID, toID int, amount float64) error {
	return repo.WithTx(ctx, func(tx *repository.TxRepository[Account, int]) error {
		// Get source account
		from, err := tx.FindByID(ctx, fromID)
		if err != nil {
			return fmt.Errorf("source account not found: %w", err)
		}

		// Check sufficient funds
		if from.Balance < amount {
			return fmt.Errorf("insufficient funds: have %.2f, need %.2f", from.Balance, amount)
		}

		// Debit source
		q, err := query.Build(
			"UPDATE accounts SET balance = balance - :amount WHERE id = :id",
			map[string]any{"id": fromID, "amount": amount},
		)
		if err != nil {
			return err
		}
		_, err = tx.Update(ctx, q)
		if err != nil {
			return fmt.Errorf("failed to debit: %w", err)
		}

		// Credit destination
		q, err = query.Build(
			"UPDATE accounts SET balance = balance + :amount WHERE id = :id",
			map[string]any{"id": toID, "amount": amount},
		)
		if err != nil {
			return err
		}
		_, err = tx.Update(ctx, q)
		if err != nil {
			return fmt.Errorf("failed to credit: %w", err)
		}

		return nil
	})
}

func printBalances(ctx context.Context, repo *repository.Repository[Account, int]) {
	accounts, err := repo.FindAll(ctx)
	if err != nil {
		log.Printf("Failed to get accounts: %v", err)
		return
	}

	fmt.Println("Current balances:")
	for _, acc := range accounts {
		fmt.Printf("  %s: $%.2f\n", acc.Name, acc.Balance)
	}
}
