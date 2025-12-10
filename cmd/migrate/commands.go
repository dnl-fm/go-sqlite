package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/fightbulc/go-turso-kit/pkg/migrations"
	_ "github.com/tursodatabase/turso-go"
)

// getDatabaseURL returns the database URL from environment
func getDatabaseURL() (string, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return "", fmt.Errorf("DATABASE_URL environment variable not set")
	}
	return url, nil
}

// openDB opens a database connection
func openDB() (*sql.DB, error) {
	url, err := getDatabaseURL()
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("turso", url)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Verify connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

// runUp runs all pending migrations
func runUp() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	fmt.Println("Running migrations...")

	if err := migrations.Run(ctx, db); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Show final status
	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	executed := 0
	for _, s := range statuses {
		if s.ExecutedAt != nil {
			executed++
		}
	}

	fmt.Printf("✓ Successfully ran migrations (%d/%d executed)\n", executed, len(statuses))

	return nil
}

// runDown rolls back migrations
func runDown(version string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	if version == "" {
		fmt.Println("Rolling back last migration...")
	} else {
		fmt.Printf("Rolling back to version %s...\n", version)
	}

	if err := migrations.Rollback(ctx, db, version); err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Println("✓ Successfully rolled back migration")

	return nil
}

// runStatus shows migration status
func runStatus() error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx := context.Background()

	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		return fmt.Errorf("failed to get status: %w", err)
	}

	if len(statuses) == 0 {
		fmt.Println("No migrations registered")
		return nil
	}

	fmt.Println("Migration Status:")
	fmt.Println()
	fmt.Printf("%-20s %-30s %-10s %-20s %s\n", "Version", "Description", "Status", "Executed At", "Duration")
	fmt.Println("─────────────────────────────────────────────────────────────────────────────────────────────")

	for _, s := range statuses {
		status := "pending"
		executedAt := "-"
		duration := "-"

		if s.ExecutedAt != nil {
			status = "✓ done"
			executedAt = s.ExecutedAt.Format("2006-01-02 15:04:05")
			duration = fmt.Sprintf("%dms", s.DurationMs)
		}

		fmt.Printf("%-20s %-30s %-10s %-20s %s\n",
			s.Version,
			truncate(s.Description, 30),
			status,
			executedAt,
			duration,
		)
	}

	// Summary
	executed := 0
	for _, s := range statuses {
		if s.ExecutedAt != nil {
			executed++
		}
	}

	fmt.Println()
	fmt.Printf("Total: %d migrations (%d executed, %d pending)\n",
		len(statuses), executed, len(statuses)-executed)

	return nil
}

// getModuleName reads the module name from go.mod in current directory
func getModuleName() (string, error) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return "", fmt.Errorf("failed to read go.mod: %w", err)
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimPrefix(line, "module "), nil
		}
	}

	return "", fmt.Errorf("module declaration not found in go.mod")
}

// runCreate generates a new migration file
func runCreate(name string) error {
	// Get module name from go.mod
	moduleName, err := getModuleName()
	if err != nil {
		return err
	}

	// Generate version (timestamp)
	version := time.Now().Format("20060102150405")

	// Prepare migration file name
	fileName := fmt.Sprintf("%s_%s.go", version, name)
	filePath := filepath.Join("migrations", fileName)

	// Check if migrations directory exists
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory not found. Create it first: mkdir migrations")
	}

	// Check if file already exists
	if _, err := os.Stat(filePath); err == nil {
		return fmt.Errorf("migration file already exists: %s", filePath)
	}

	// Create migration file from template
	tmpl, err := template.New("migration").Parse(migrationTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse template: %w", err)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	data := struct {
		Version     string
		Description string
		ModuleName  string
	}{
		Version:     version,
		Description: name,
		ModuleName:  moduleName,
	}

	if err := tmpl.Execute(file, data); err != nil {
		return fmt.Errorf("failed to write template: %w", err)
	}

	fmt.Printf("Created migration: %s\n", filePath)
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("1. Edit the migration file and implement the Up and Down functions")
	fmt.Printf("2. Import the migration package in your main.go:\n")
	fmt.Printf("   import _ \"%s/migrations\"\n", moduleName)
	fmt.Println("3. Run migrations:")
	fmt.Println("   migrate up")

	return nil
}

// truncate truncates a string to maxLen
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// migrationTemplate is the template for new migration files
const migrationTemplate = `package migrations

import (
	"context"
	"database/sql"

	"github.com/fightbulc/go-turso-kit/pkg/migrations"
)

// Package import path: {{.ModuleName}}/migrations

func init() {
	migrations.Register(migrations.Migration{
		Version:     "{{.Version}}",
		Description: "{{.Description}}",
		Up:          up{{.Version}},
		Down:        down{{.Version}},
	})
}

// up{{.Version}} runs the up migration
func up{{.Version}}(ctx context.Context, db *sql.DB) error {
	// TODO: Implement up migration
	// Example:
	// _, err := db.ExecContext(ctx, ` + "`" + `
	// 	CREATE TABLE example (
	// 		id TEXT PRIMARY KEY,
	// 		name TEXT NOT NULL,
	// 		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	// 	)
	// ` + "`" + `)
	// return err

	return nil
}

// down{{.Version}} rolls back the migration
func down{{.Version}}(ctx context.Context, db *sql.DB) error {
	// TODO: Implement down migration
	// Example:
	// _, err := db.ExecContext(ctx, ` + "`" + `DROP TABLE example` + "`" + `)
	// return err

	return nil
}
`
