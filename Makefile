.PHONY: build test lab-test lint backpressure check check-dev file-size test-cover test-verbose clean migrate-build

# Build migrate CLI
build:
	@echo "Building migrate CLI..."
	@GOWORK=off go build -o bin/migrate ./cmd/migrate

# Run all tests
test:
	@GOWORK=off go test ./pkg/...

# Run exploratory labs
lab-test:
	@cd lab/turso-v060 && GOWORK=off go test ./...

# Run deterministic lint checks
lint:
	@GOWORK=off go vet ./...
	@GOWORK=off golangci-lint run --new --disable=intrange --disable=ireturn --disable=noinlineerr --disable=unqueryvet

# Run local quality backpressure
backpressure: lint file-size test build

# Run full repository policy and local backpressure
check:
	@rabbit check
	@$(MAKE) backpressure

# Run development-phase checks. Rabbit still runs, but selected rules are
# downgraded to warnings so policy churn stays visible without blocking flow.
check-dev:
	@rabbit check --dev --dev-allow "$(RABBIT_DEV_ALLOW)"
	@$(MAKE) backpressure

file-size:
	@./scripts/check-file-size.sh

# Run tests with coverage
test-cover:
	@GOWORK=off go test ./pkg/... -cover

# Run tests with verbose output
test-verbose:
	@GOWORK=off go test ./pkg/... -v

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
