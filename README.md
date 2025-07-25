# Go Circuit Breaker

A Go library demonstrating resilient service communication patterns including circuit breakers, retry mechanisms, and failure simulation with Redis integration.

## Features

- **Circuit Breaker Pattern**: Prevents cascading failures with configurable thresholds
- **Retry Logic with Backoff**: Exponential backoff with jitter for optimal retry patterns
- **Service Simulations**: Multiple failure scenarios to test retry behavior
    - Unreliable services with configurable failure rates
    - Slow services with variable response times
    - Recovering services that succeed after N attempts
- **Redis Integration**: Demonstrates retry patterns with external dependencies
- **Docker Support**: Containerized Redis for easy local development
- **Comprehensive Testing**: Unit tests and integration tests with coverage reports
- **Benchmarking**: Performance testing for retry mechanisms

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Docker and Docker Compose
- Make (optional, but recommended)

### Quick Start

1. **Clone and setup**:
   ```bash
   git clone <your-repo>
   cd go-circuit-breaker
   make dev  # Downloads dependencies and formats code
   ```

2. **Start Redis and run the application**:
   ```bash
   make run-with-redis
   ```

3. **Run all tests**:
   ```bash
   make test-integration
   ```

### Manual Setup (without Make)

```bash
# Start Redis container
docker run -d --name redis-test -p 6379:6379 redis:7-alpine

# Build and run
go build -o bin/go-circuit-breaker .
REDIS_URL=localhost:6379 ./bin/go-circuit-breaker
```

## Running Simulations

### All Simulation Scenarios
```bash
make simulate-all
```

### Individual Scenarios

**Unreliable Service** (30% failure rate):
```bash
make docker-up
REDIS_URL=localhost:6379 SCENARIO=unreliable go run .
```

**Slow Service** (200ms-2s response times):
```bash
REDIS_URL=localhost:6379 SCENARIO=slow go run .
```

**Recovering Service** (fails first 3 attempts):
```bash
REDIS_URL=localhost:6379 SCENARIO=recovering go run .
```

## Available Commands

### Development
- `make dev` - Setup development environment
- `make build` - Build the application
- `make run` - Build and run the application
- `make test` - Run all tests
- `make test-cover` - Run tests with coverage report

### Docker Management
- `make docker-up` - Start Redis container
- `make docker-down` - Stop Redis container
- `make docker-logs` - View Redis logs
- `make docker-shell` - Connect to Redis CLI

### Testing & Simulation
- `make test-integration` - Run integration tests with Redis
- `make simulate-all` - Run all simulation scenarios
- `make benchmark` - Run performance benchmarks

### Utilities
- `make clean` - Clean build artifacts
- `make clean-all` - Clean everything including Docker containers
- `make help` - Show all available commands

## Project Structure

```
├── main.go              # Main application with circuit breaker and retry logic
├── *_test.go           # Test files
├── Makefile            # Build and development automation
├── go.mod              # Go module dependencies
└── bin/                # Built binaries (created after build)
```

## Integration Examples

### HTTP Service Integration

```go
package main

import (
    "context"
    "fmt"
    "time"
)

func main() {
    // Create HTTP client with circuit breaker and retry
    client := NewHTTPClient(5 * time.Second)
    
    // Use in your application
    ctx := context.Background()
    resp, err := client.Get(ctx, "https://api.example.com/users/1")
    if err != nil {
        fmt.Printf("Request failed: %v\n", err)
        return
    }
    defer resp.Body.Close()
    
    fmt.Printf("Response: %s\n", resp.Status)
}
```

The HTTP client automatically handles:
- **Circuit breaker**: Prevents cascade failures after 5 consecutive failures
- **Retry logic**: 3 attempts with exponential backoff (100ms to 5s)
- **Timeout handling**: Configurable per-request timeouts
- **Context support**: Proper cancellation and deadline handling

See `examples/http_integration.go` for a complete implementation.

### Database Integration

```go
// Example with database operations
func queryWithResilience(db *sql.DB, query string) error {
    operation := func() error {
        _, err := db.Exec(query)
        return err
    }
    
    return retryWithCircuitBreaker(operation, retryConfig{
        maxAttempts: 3,
        baseDelay:   200 * time.Millisecond,
    })
}
```

## Configuration

Environment variables:
- `REDIS_URL` - Redis connection string (default: localhost:6379)
- `SCENARIO` - Simulation scenario: unreliable, slow, recovering

## Testing

Run different types of tests:

```bash
# Unit tests only
go test ./...

# Integration tests with Redis
make test-integration

# Tests with coverage
make test-cover

# Benchmarks
make benchmark
```

## Contributing

1. Run `make dev` to set up the development environment
2. Make your changes
3. Run `make ci` to ensure all checks pass
4. Submit a pull request

## License

MIT License