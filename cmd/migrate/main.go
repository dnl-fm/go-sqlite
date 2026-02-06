package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]
	var err error

	switch command {
	case "up":
		err = runUp()
	case "down":
		version := ""
		if len(os.Args) > 2 {
			version = os.Args[2]
		}
		err = runDown(version)
	case "status":
		err = runStatus()
	case "create":
		if len(os.Args) < 3 {
			fmt.Fprintf(os.Stderr, "Error: migration name required\n")
			printUsage()
			os.Exit(1)
		}
		err = runCreate(os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("Usage: migrate <command> [arguments]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  up              Run all pending migrations")
	fmt.Println("  down [version]  Rollback to version (or last migration if not specified)")
	fmt.Println("  status          Show migration status")
	fmt.Println("  create <name>   Generate new migration file template")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  DATABASE_URL    Database connection string (required)")
	fmt.Println()
	fmt.Println("Example:")
	fmt.Println("  export DATABASE_URL=\"file:./app.db\"")
	fmt.Println("  migrate up")
	fmt.Println("  migrate status")
	fmt.Println("  migrate create add_users_table")
	fmt.Println("  migrate down 20251107000001")
}
