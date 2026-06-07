# PiWorker Makefile
# Build, test, and development utilities

.PHONY: all build test test-coverage test-race test-short test-verbose \
        clean lint fmt vet deps help hooks-install \
        docker-build docker-run docker-dev docker-dev-down docker-prod docker-prod-down \
        frontend-build frontend-dev frontend-install frontend-typecheck

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOVET=$(GOCMD) vet
GOFMT=gofmt

# Build output
BINARY_NAME=piworker
BUILD_DIR=build

# Test parameters
TEST_FLAGS=-v
COVERAGE_FILE=coverage.out
COVERAGE_HTML=coverage.html

# Default target
all: test build

## Build targets

build: ## Build the binary
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) ./...

build-linux: ## Build for Linux
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./...

build-arm: ## Build for ARM (Raspberry Pi)
	GOOS=linux GOARCH=arm $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm ./...

build-arm64: ## Build for ARM64
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./...

## Test targets

test: ## Run all tests
	$(GOTEST) ./...

test-verbose: ## Run all tests with verbose output
	$(GOTEST) -v ./...

test-short: ## Run short tests only
	$(GOTEST) -short ./...

test-race: ## Run tests with race detector
	$(GOTEST) -race ./...

test-coverage: ## Run tests with coverage report
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) ./...
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Coverage report generated: $(COVERAGE_HTML)"

test-coverage-func: ## Run tests and show coverage by function
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) ./...
	$(GOCMD) tool cover -func=$(COVERAGE_FILE)

test-integration: ## Run integration tests only
	$(GOTEST) -tags=integration ./...

test-unit: ## Run unit tests only (exclude integration)
	$(GOTEST) -short ./...

## Test specific packages

test-flow: ## Run flow package tests
	$(GOTEST) -v ./internal/flow/...

test-node: ## Run node package tests
	$(GOTEST) -v ./internal/node/...

test-storage: ## Run storage package tests
	$(GOTEST) -v ./internal/storage/...

test-api: ## Run API package tests
	$(GOTEST) -v ./internal/api/...

test-builtin: ## Run builtin nodes tests
	$(GOTEST) -v ./internal/node/builtin/...

## Code quality targets

lint: ## Run linter (requires golangci-lint)
	golangci-lint run ./...

fmt: ## Format code
	$(GOFMT) -s -w .

fmt-check: ## Check code formatting
	@if [ -n "$$($(GOFMT) -l .)" ]; then \
		echo "Code is not properly formatted. Run 'make fmt' to fix."; \
		$(GOFMT) -l .; \
		exit 1; \
	fi

vet: ## Run go vet
	$(GOVET) ./...

hooks-install: ## Install git hooks (auto-gofmt on commit)
	@git config core.hooksPath scripts/hooks
	@chmod +x scripts/hooks/pre-commit
	@echo "Git hooks installed (core.hooksPath -> scripts/hooks)"

check: fmt-check vet lint frontend-typecheck ## Run all checks

## Dependency management

deps: ## Download dependencies
	$(GOMOD) download

deps-tidy: ## Tidy dependencies
	$(GOMOD) tidy

deps-verify: ## Verify dependencies
	$(GOMOD) verify

deps-update: ## Update all dependencies
	$(GOGET) -u ./...
	$(GOMOD) tidy

## Cleanup targets

clean: ## Clean build artifacts
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f $(COVERAGE_FILE) $(COVERAGE_HTML)

clean-test: ## Clean test cache
	$(GOCMD) clean -testcache

## Development utilities

run: build ## Build and run
	./$(BUILD_DIR)/$(BINARY_NAME)

watch: ## Watch for changes and run tests (requires entr)
	find . -name '*.go' | entr -c make test

bench: ## Run benchmarks
	$(GOTEST) -bench=. -benchmem ./...

bench-cpu: ## Run benchmarks with CPU profile
	$(GOTEST) -bench=. -cpuprofile=cpu.prof ./...

bench-mem: ## Run benchmarks with memory profile
	$(GOTEST) -bench=. -memprofile=mem.prof ./...

## Documentation

docs: ## Generate documentation (requires godoc)
	godoc -http=:6060

## Docker targets

docker-build: ## Build production Docker image
	docker build -t piworker:latest .

docker-run: docker-build ## Build and run production container
	docker run -d --name piworker -p 8080:8080 -v piworker-data:/app/data piworker:latest

docker-dev: ## Start development environment with Docker Compose
	docker-compose up -d
	@echo "Development environment started:"
	@echo "  - Backend:  http://localhost:8080"
	@echo "  - Frontend: http://localhost:5173"

docker-dev-down: ## Stop development environment
	docker-compose down

docker-dev-logs: ## Show development logs
	docker-compose logs -f

docker-prod: ## Start production environment with Docker Compose
	docker-compose -f docker-compose.prod.yml up -d
	@echo "Production environment started: http://localhost:8080"

docker-prod-down: ## Stop production environment
	docker-compose -f docker-compose.prod.yml down

docker-clean: ## Remove all PiWorker Docker resources
	docker-compose down -v 2>/dev/null || true
	docker-compose -f docker-compose.prod.yml down -v 2>/dev/null || true
	docker rmi piworker:latest 2>/dev/null || true

## Frontend targets
## Uses Bun (https://bun.sh) as runtime and package manager
## Vite is still needed for Vue SFC support and HMR

frontend-install: ## Install dependencies (bun install)
	cd web && bun install

frontend-dev: ## Start dev server (bun + vite)
	cd web && bun run dev

frontend-typecheck: ## Run Vue/TypeScript type checking
	cd web && bun run typecheck

frontend-build: ## Build for production (bun + vite)
	cd web && bun run build

frontend-embed: frontend-build ## Build and embed frontend in Go binary
	rm -rf internal/webui/dist/*
	cp -r web/dist/* internal/webui/dist/
	@echo "Frontend embedded in internal/webui/dist/"

## Full build targets

build-all: frontend-embed build ## Build everything (frontend + backend)
	@echo "Full build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-release: frontend-embed ## Build release binary with embedded frontend
	CGO_ENABLED=1 $(GOBUILD) -ldflags="-w -s" -o $(BUILD_DIR)/$(BINARY_NAME) .
	@echo "Release binary: $(BUILD_DIR)/$(BINARY_NAME)"

## Help target

help: ## Display this help
	@echo "PiWorker - Visual Flow-Based Automation System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
