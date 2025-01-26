# Circuit Breaker Library with Redis Adapter

A scalable circuit breaker library in Go, with support for Redis-based state storage.

## Features
- Core circuit breaker logic.
- In-memory and Redis adapters for state storage.
- Example usage in `main.go`.

## Usage
1. Clone the repository.
2. Run Redis:
   ```bash
   docker run -d -p 6379:6379 redis