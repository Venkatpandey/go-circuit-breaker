APP_NAME := http-demo
BIN_DIR := bin
BIN_PATH := $(BIN_DIR)/$(APP_NAME)
GO ?= go
GOFLAGS ?=
GOCACHE_DIR := $(CURDIR)/.gocache
GOTMP_DIR := $(CURDIR)/.gotmp
GOENV := GOCACHE=$(GOCACHE_DIR) GOTMPDIR=$(GOTMP_DIR)
REDIS_PORT := 6379

.PHONY: build test test-race test-integration vet lint vuln benchmark demo clean docker-up docker-down help ci

help:
	@echo "Available targets:"
	@echo "  build             Build the HTTP demo binary"
	@echo "  test              Run the default unit test suite"
	@echo "  test-race         Run tests with the race detector"
	@echo "  test-integration  Run Redis adapter integration tests"
	@echo "  vet               Run go vet"
	@echo "  lint              Run static analysis (staticcheck)"
	@echo "  vuln              Run vulnerability scan (govulncheck)"
	@echo "  benchmark         Run the local benchmark suite"
	@echo "  demo              Run the HTTP demo"
	@echo "  docker-up         Start local Redis via docker-compose"
	@echo "  docker-down       Stop local Redis"
	@echo "  clean             Remove build and cache artifacts"
	@echo "  ci                Run the local CI workflow"

$(GOCACHE_DIR) $(GOTMP_DIR) $(BIN_DIR):
	@mkdir -p $@

build: $(GOCACHE_DIR) $(GOTMP_DIR) $(BIN_DIR)
	env $(GOENV) $(GO) build $(GOFLAGS) -o $(BIN_PATH) ./cmd/http-demo

test: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) test $(GOFLAGS) ./...

test-race: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) test $(GOFLAGS) -race ./...

test-integration: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) test $(GOFLAGS) -tags=integration ./...

vet: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) vet $(GOFLAGS) ./...

lint: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) run honnef.co/go/tools/cmd/staticcheck@latest ./...

vuln: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) run golang.org/x/vuln/cmd/govulncheck@latest ./...

benchmark: $(GOCACHE_DIR) $(GOTMP_DIR)
	env $(GOENV) $(GO) test $(GOFLAGS) -run=^$$ -bench=. -benchmem ./core ./service

demo: build
	./$(BIN_PATH)

docker-up:
	docker-compose up -d redis

docker-down:
	docker-compose down

clean:
	rm -rf $(BIN_DIR) .gocache .gotmp coverage.out

ci: test test-race test-integration vet lint vuln build
