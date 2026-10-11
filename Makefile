.PHONY: help build consumer-build consumer-scan test lab-test lab-memory lab-writers lab-tenants lint backpressure check check-dev rabbit-check rabbit-check-dev rabbit-review file-size test-cover test-verbose install-hooks clean migrate-build migrate-up migrate-down migrate-status migrate-create

TURSO_VERSIONS ?= v0.7.2 v0.8.1

CONSUMER_ROOT ?= $(HOME)/werk

help:
	@echo ""
	@echo "github.com/dnl-fm/go-sqlite"
	@echo ""
	@echo "  make build           Build all CLIs"
	@echo "  make consumer-build  Build the consumer scanner"
	@echo "  make consumer-scan   Scan CONSUMER_ROOT for go-sqlite users"
	@echo "  make test            Run package tests"
	@echo "  make lab-test        Run modernc SQLite labs (short mode)"
	@echo "  make lab-memory      Evaluate raw Turso native memory (historical lab)"
	@echo "  make lab-writers     Full SQLite writer scenarios: patterns, WAL, load (slow)"
	@echo "  make lab-tenants     Full SQLite tenant-per-database scenarios (slow)"
	@echo "  make lint            Run deterministic lint checks"
	@echo "  make backpressure    Run local quality backpressure"
	@echo "  make check           Run full repository checks"
	@echo "  make check-dev       Run development-phase checks"
	@echo "  make clean           Remove build artifacts"
	@echo ""

build:
	@echo ""
	@$(MAKE) migrate-build consumer-build
	@echo "Built all CLIs"
	@echo ""

consumer-build:
	@echo ""
	@GOWORK=off go build -o bin/go-sqlite-consumers ./cmd/go-sqlite-consumers
	@echo "Built bin/go-sqlite-consumers"
	@echo ""

consumer-scan:
	@echo ""
	@GOWORK=off go run ./cmd/go-sqlite-consumers scan "$(CONSUMER_ROOT)" $(ARGS)
	@echo ""

test:
	@echo ""
	@GOWORK=off go test ./...
	@echo ""

lab-memory:
	@cd lab/turso-memory && ./run.sh $(TURSO_VERSIONS)

lab-writers:
	@cd lab/sqlite-writers && LAB_FULL=1 GOWORK=off go test -count=1 -run Scenario -v -timeout 90m ./...

lab-tenants:
	@cd lab/sqlite-tenants && LAB_FULL=1 GOWORK=off go test -count=1 -run Scenario -v -timeout 30m ./...

lab-test:
	@echo ""
	@cd lab/sqlite-writers && GOWORK=off go test ./...
	@cd lab/sqlite-tenants && GOWORK=off go test ./...
	@echo ""

lint:
	@echo ""
	@GOWORK=off go vet ./...
	@GOWORK=off golangci-lint run --new --disable=intrange --disable=ireturn --disable=noinlineerr --disable=unqueryvet
	@echo ""

backpressure:
	@echo ""
	@$(MAKE) lint file-size test build rabbit-check
	@echo "Backpressure passed"
	@echo ""

check:
	@echo ""
	@$(MAKE) backpressure
	@echo "Check passed"
	@echo ""

check-dev:
	@echo ""
	@$(MAKE) lint file-size test build rabbit-check-dev
	@echo "Development checks passed"
	@echo ""

rabbit-check:
	@echo ""
	@rabbit check --ci
	@echo ""

rabbit-check-dev:
	@echo ""
	@rabbit check --dev --dev-allow "$(RABBIT_DEV_ALLOW)"
	@echo ""

rabbit-review:
	@echo ""
	@rabbit review
	@echo ""

file-size:
	@echo ""
	@./scripts/check-file-size.sh
	@echo ""

test-cover:
	@echo ""
	@GOWORK=off go test ./... -cover
	@echo ""

test-verbose:
	@echo ""
	@GOWORK=off go test ./... -v
	@echo ""

install-hooks:
	@echo ""
	@./scripts/install-git-hooks.sh
	@echo ""

clean:
	@echo ""
	@rm -rf bin/
	@echo "Cleaned build artifacts"
	@echo ""

migrate-build:
	@echo ""
	@GOWORK=off go build -o bin/migrate ./cmd/migrate
	@echo "Built bin/migrate"
	@echo ""

migrate-up:
	@echo ""
	@$(MAKE) migrate-build
	@./bin/migrate up
	@echo ""

migrate-down:
	@echo ""
	@$(MAKE) migrate-build
	@./bin/migrate down
	@echo ""

migrate-status:
	@echo ""
	@$(MAKE) migrate-build
	@./bin/migrate status
	@echo ""

migrate-create:
	@echo ""
	@if [ -z "$(name)" ]; then \
		echo "Error: migration name required. Usage: make migrate-create name=migration_name"; \
		echo ""; \
		exit 1; \
	fi
	@$(MAKE) migrate-build
	@./bin/migrate create $(name)
	@echo ""
