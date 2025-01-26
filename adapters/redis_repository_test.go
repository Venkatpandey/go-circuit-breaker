package adapters

import (
	"context"
	"github.com/go-redis/redis/v8"
	"go-circuit-breaker/core"
	"testing"
	"time"
)

func TestRedisRepository_SaveAndFindByID(t *testing.T) {
	// Initialize Redis client
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	// Ping Redis to check the connection
	ctx := context.Background()
	_, err := client.Ping(ctx).Result()
	if err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	repo := NewRedisRepository(client)

	cb := core.NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)
	cb.State = core.StateOpen

	// Save the circuit breaker
	err = repo.Save(cb)
	if err != nil {
		t.Errorf("Failed to save circuit breaker: %v", err)
	}

	// Retrieve the circuit breaker
	retrievedCB, err := repo.FindByID("default")
	if err != nil {
		t.Errorf("Failed to find circuit breaker: %v", err)
	}

	if retrievedCB == nil {
		t.Error("Expected circuit breaker to be found, got nil")
	}

	if retrievedCB.State != core.StateOpen {
		t.Errorf("Expected state to be Open, got %v", retrievedCB.State)
	}
}
