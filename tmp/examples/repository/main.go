package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	_ "github.com/fightbulc/go-turso-kit/pkg/driver/modernc"

	"github.com/fightbulc/go-turso-kit/pkg/query"
	"github.com/fightbulc/go-turso-kit/pkg/repository"
)

// User is an example entity with db struct tags
type User struct {
	ID    int    `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func main() {
	ctx := context.Background()

	// Open in-memory database
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	// Create users table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			email TEXT NOT NULL UNIQUE
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}

	// Create repository - no mapper needed, uses db struct tags
	userRepo := repository.New[User, int](db, "users")

	// Insert users
	fmt.Println("=== Inserting Users ===")
	err = insertUser(ctx, db, "Alice", "alice@example.com")
	if err != nil {
		log.Fatalf("Failed to insert user: %v", err)
	}
	fmt.Println("✓ Inserted Alice")

	err = insertUser(ctx, db, "Bob", "bob@example.com")
	if err != nil {
		log.Fatalf("Failed to insert user: %v", err)
	}
	fmt.Println("✓ Inserted Bob")

	// Find all users
	fmt.Println("\n=== Finding All Users ===")
	users, err := userRepo.FindAll(ctx)
	if err != nil {
		log.Fatalf("Failed to find all users: %v", err)
	}
	for _, user := range users {
		fmt.Printf("  ID: %d, Name: %s, Email: %s\n", user.ID, user.Name, user.Email)
	}

	// Find by ID
	fmt.Println("\n=== Finding User by ID ===")
	user, err := userRepo.FindByID(ctx, 1)
	if err != nil {
		log.Fatalf("Failed to find user: %v", err)
	}
	fmt.Printf("Found: %s <%s>\n", user.Name, user.Email)

	// Update user using query builder
	fmt.Println("\n=== Updating User ===")
	q, err := query.Build(
		"UPDATE users SET name = :name, email = :email WHERE id = :id",
		map[string]any{"id": 1, "name": "Alicia", "email": "alicia@example.com"},
	)
	if err != nil {
		log.Fatalf("Failed to build query: %v", err)
	}
	_, err = userRepo.Update(ctx, q)
	if err != nil {
		log.Fatalf("Failed to update user: %v", err)
	}
	fmt.Println("✓ Updated Alice to Alicia")

	// Verify update
	user, err = userRepo.FindByID(ctx, 1)
	if err != nil {
		log.Fatalf("Failed to find updated user: %v", err)
	}
	fmt.Printf("Verified: %s <%s>\n", user.Name, user.Email)

	// Count users
	fmt.Println("\n=== Counting Users ===")
	count, err := userRepo.Count(ctx)
	if err != nil {
		log.Fatalf("Failed to count users: %v", err)
	}
	fmt.Printf("Total users: %d\n", count)

	// Check if user exists
	fmt.Println("\n=== Checking if User Exists ===")
	exists, err := userRepo.Exists(ctx, 1)
	if err != nil {
		log.Fatalf("Failed to check existence: %v", err)
	}
	fmt.Printf("User 1 exists: %v\n", exists)

	// Find by query
	fmt.Println("\n=== Finding Users by Query ===")
	q, err = query.Build(
		"SELECT * FROM users WHERE email LIKE :pattern",
		map[string]any{"pattern": "%@example.com"},
	)
	if err != nil {
		log.Fatalf("Failed to build query: %v", err)
	}
	users, err = userRepo.FindByQuery(ctx, q)
	if err != nil {
		log.Fatalf("Failed to find by query: %v", err)
	}
	fmt.Printf("Found %d users with @example.com emails\n", len(users))

	// Delete user
	fmt.Println("\n=== Deleting User ===")
	err = userRepo.DeleteByID(ctx, 1)
	if err != nil {
		log.Fatalf("Failed to delete user: %v", err)
	}
	fmt.Println("✓ Deleted user 1")

	// Verify deletion
	users, err = userRepo.FindAll(ctx)
	if err != nil {
		log.Fatalf("Failed to find all users: %v", err)
	}
	fmt.Printf("Remaining users: %d\n", len(users))

	// Transaction example
	fmt.Println("\n=== Transaction Example ===")
	err = userRepo.WithTx(ctx, func(txRepo *repository.TxRepository[User, int]) error {
		// Insert in transaction
		q, err := query.Build(
			"INSERT INTO users (name, email) VALUES (:name, :email)",
			map[string]any{"name": "Charlie", "email": "charlie@example.com"},
		)
		if err != nil {
			return err
		}
		_, err = txRepo.Insert(ctx, q)
		if err != nil {
			return err
		}
		fmt.Println("  ✓ Inserted Charlie in transaction")

		// Insert another
		q, err = query.Build(
			"INSERT INTO users (name, email) VALUES (:name, :email)",
			map[string]any{"name": "Diana", "email": "diana@example.com"},
		)
		if err != nil {
			return err
		}
		_, err = txRepo.Insert(ctx, q)
		if err != nil {
			return err
		}
		fmt.Println("  ✓ Inserted Diana in transaction")

		return nil
	})
	if err != nil {
		log.Fatalf("Transaction failed: %v", err)
	}
	fmt.Println("✓ Transaction committed")

	// Verify transaction
	users, err = userRepo.FindAll(ctx)
	if err != nil {
		log.Fatalf("Failed to find all users: %v", err)
	}
	fmt.Printf("Final user count: %d\n", len(users))
	for _, user := range users {
		fmt.Printf("  - %s\n", user.Name)
	}

	fmt.Println("\n✅ Repository example complete!")
}

// Helper function for initial inserts
func insertUser(ctx context.Context, db *sql.DB, name, email string) error {
	_, err := db.ExecContext(ctx,
		"INSERT INTO users (name, email) VALUES (?, ?)",
		name, email,
	)
	return err
}
