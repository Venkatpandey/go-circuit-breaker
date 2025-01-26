package main

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"go-circuit-breaker/adapters"
	"go-circuit-breaker/service"
	"time"
)

func main() {
	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis server address
		Password: "",               // No password
		DB:       0,                // Default DB
	})

	// Ping Redis to check the connection
	ctx := context.Background()
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		panic(fmt.Sprintf("failed to connect to Redis: %v", err))
	}

	// Create the Redis repository
	repo := adapters.NewRedisRepository(redisClient)

	// Create the circuit breaker breakerService
	breakerService := service.NewCircuitBreakerService(repo)

	// Simulate a failing breakerService
	for i := 0; i < 10; i++ {
		err := breakerService.Execute("default", func() error {
			if i < 5 {
				return fmt.Errorf("breakerService error")
			}
			return nil
		})

		if err != nil {
			fmt.Printf("Attempt %d: %v\n", i+1, err)
		} else {
			fmt.Printf("Attempt %d: success\n", i+1)
		}

		time.Sleep(1 * time.Second)
	}
}
