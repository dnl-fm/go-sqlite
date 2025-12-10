.PHONY: build test test-cover test-verbose clean migrate-build

# Build migrate CLI
build:
	@echo "Building migrate CLI..."
	@go build -o bin/migrate ./cmd/migrate

# Run all tests
test:
	@go test ./pkg/...

# Run tests with coverage
test-cover:
	@go test ./pkg/... -cover

# Run tests with verbose output
test-verbose:
	@go test ./pkg/... -v

# Clean build artifacts
clean:
	@rm -rf bin/

# Build migrate CLI (alias)
migrate-build: build

# ---------------------------------------------------------
# Migration Commands (require DATABASE_URL to be set)
# ---------------------------------------------------------

# Run pending migrations
migrate-up: build
	@./bin/migrate up

# Rollback last migration
migrate-down: build
	@./bin/migrate down

# Show migration status
migrate-status: build
	@./bin/migrate status

# Create new migration: make migrate-create name=add_users_table
migrate-create: build
	@if [ -z "$(name)" ]; then \
		echo "Error: migration name required. Usage: make migrate-create name=migration_name"; \
		exit 1; \
	fi
	@./bin/migrate create $(name)
