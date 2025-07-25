# Go project Makefile with Redis Docker support

# Variables
APP_NAME := go-circuit-breaker
GO_VERSION := 1.21
REDIS_CONTAINER := redis-test
REDIS_PORT := 6379
DOCKER_NETWORK := retry-net

# Colors for output
RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
NC := \033[0m # No Color

.PHONY: help build run test clean docker-up docker-down docker-logs deps fmt vet lint all

# Default target
all: deps fmt vet test build

help: ## Show this help message
	@echo "$(BLUE)Available targets:$(NC)"
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ { printf "  $(GREEN)%-15s$(NC) %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

deps: ## Download and tidy dependencies
	@echo "$(YELLOW)Downloading dependencies...$(NC)"
	go mod download
	go mod tidy

fmt: ## Format Go code
	@echo "$(YELLOW)Formatting code...$(NC)"
	go fmt ./...

vet: ## Run go vet
	@echo "$(YELLOW)Running go vet...$(NC)"
	go vet ./...

lint: ## Run golangci-lint (requires golangci-lint to be installed)
	@echo "$(YELLOW)Running linter...$(NC)"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "$(RED)golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest$(NC)"; \
	fi

build: ## Build the application
	@echo "$(YELLOW)Building application...$(NC)"
	go build -o bin/$(APP_NAME) .
	@echo "$(GREEN)Build complete: bin/$(APP_NAME)$(NC)"

test: ## Run all tests
	@echo "$(YELLOW)Running tests...$(NC)"
	go test -v ./...

test-cover: ## Run tests with coverage
	@echo "$(YELLOW)Running tests with coverage...$(NC)"
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)Coverage report generated: coverage.html$(NC)"

run: build ## Build and run the application
	@echo "$(YELLOW)Running application...$(NC)"
	./bin/$(APP_NAME)

run-dev: ## Run without building (go run)
	@echo "$(YELLOW)Running in dev mode...$(NC)"
	go run .

# Docker targets
docker-network: ## Create Docker network if it doesn't exist
	@docker network inspect $(DOCKER_NETWORK) >/dev/null 2>&1 || \
		(echo "$(YELLOW)Creating Docker network: $(DOCKER_NETWORK)$(NC)" && \
		docker network create $(DOCKER_NETWORK))

docker-up: docker-network ## Start Redis container
	@echo "$(YELLOW)Starting Redis container...$(NC)"
	@if [ "$$(docker ps -aq -f name=$(REDIS_CONTAINER))" ]; then \
		if [ "$$(docker ps -q -f name=$(REDIS_CONTAINER))" ]; then \
			echo "$(GREEN)Redis container already running$(NC)"; \
		else \
			echo "$(YELLOW)Starting existing Redis container...$(NC)"; \
			docker start $(REDIS_CONTAINER); \
		fi \
	else \
		echo "$(YELLOW)Creating new Redis container...$(NC)"; \
		docker run -d \
			--name $(REDIS_CONTAINER) \
			--network $(DOCKER_NETWORK) \
			-p $(REDIS_PORT):6379 \
			redis:7-alpine \
			redis-server --appendonly yes; \
	fi
	@echo "$(GREEN)Redis is running on port $(REDIS_PORT)$(NC)"

docker-down: ## Stop and remove Redis container
	@echo "$(YELLOW)Stopping Redis container...$(NC)"
	@docker stop $(REDIS_CONTAINER) 2>/dev/null || true
	@docker rm $(REDIS_CONTAINER) 2>/dev/null || true
	@echo "$(GREEN)Redis container stopped and removed$(NC)"

docker-logs: ## Show Redis container logs
	@echo "$(YELLOW)Redis container logs:$(NC)"
	docker logs -f $(REDIS_CONTAINER)

docker-shell: ## Connect to Redis CLI
	@echo "$(YELLOW)Connecting to Redis CLI...$(NC)"
	docker exec -it $(REDIS_CONTAINER) redis-cli

docker-status: ## Check Redis container status
	@echo "$(YELLOW)Redis container status:$(NC)"
	@docker ps -f name=$(REDIS_CONTAINER) --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Integration test targets
test-integration: docker-up ## Run integration tests with Redis
	@echo "$(YELLOW)Waiting for Redis to be ready...$(NC)"
	@sleep 2
	@echo "$(YELLOW)Running integration tests...$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) go test -v -tags=integration ./...

run-with-redis: docker-up build ## Start Redis and run the application
	@echo "$(YELLOW)Waiting for Redis to be ready...$(NC)"
	@sleep 2
	@echo "$(YELLOW)Running application with Redis...$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) ./bin/$(APP_NAME)

run-dev-with-redis: docker-up ## Start Redis and run in dev mode
	@echo "$(YELLOW)Waiting for Redis to be ready...$(NC)"
	@sleep 2
	@echo "$(YELLOW)Running in dev mode with Redis...$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) go run .

# Simulation targets
simulate-all: docker-up build ## Run all simulation scenarios
	@echo "$(YELLOW)Running all simulation scenarios...$(NC)"
	@sleep 2
	@echo "$(BLUE)=== Unreliable Service Test ===$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) SCENARIO=unreliable ./bin/$(APP_NAME)
	@echo "$(BLUE)=== Slow Service Test ===$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) SCENARIO=slow ./bin/$(APP_NAME)
	@echo "$(BLUE)=== Recovering Service Test ===$(NC)"
	REDIS_URL=localhost:$(REDIS_PORT) SCENARIO=recovering ./bin/$(APP_NAME)

benchmark: ## Run benchmarks
	@echo "$(YELLOW)Running benchmarks...$(NC)"
	go test -bench=. -benchmem ./...

# Clean targets
clean: ## Clean build artifacts and stop containers
	@echo "$(YELLOW)Cleaning up...$(NC)"
	rm -rf bin/
	rm -f coverage.out coverage.html
	go clean ./...

clean-all: clean docker-down ## Clean everything including Docker containers
	@echo "$(YELLOW)Removing Docker network...$(NC)"
	@docker network rm $(DOCKER_NETWORK) 2>/dev/null || true
	@echo "$(GREEN)Cleanup complete$(NC)"

# Development workflow
dev: deps fmt vet ## Prepare for development
	@echo "$(GREEN)Development environment ready$(NC)"

ci: deps fmt vet test build ## Run CI pipeline locally
	@echo "$(GREEN)CI pipeline completed successfully$(NC)"

# Quick commands
up: docker-up ## Alias for docker-up
down: docker-down ## Alias for docker-down
logs: docker-logs ## Alias for docker-logs

# Health check
health: ## Check if Redis is healthy
	@echo "$(YELLOW)Checking Redis health...$(NC)"
	@if docker exec $(REDIS_CONTAINER) redis-cli ping >/dev/null 2>&1; then \
		echo "$(GREEN)Redis is healthy$(NC)"; \
	else \
		echo "$(RED)Redis is not responding$(NC)"; \
		exit 1; \
	fi