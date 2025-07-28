package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"go-circuit-breaker/adapters"
	"go-circuit-breaker/core"
	"go-circuit-breaker/service"
)

func main() {
	fmt.Println("🔄 Circuit Breaker Demo Starting...")
	fmt.Println("=====================================")

	// Initialize Redis client
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379", // Redis server address
		Password: "",               // No password
		DB:       0,                // Default DB
	})
	defer redisClient.Close()

	// Test Redis connection
	ctx := context.Background()
	_, err := redisClient.Ping(ctx).Result()
	if err != nil {
		panic(fmt.Sprintf("❌ Failed to connect to Redis: %v", err))
	}
	fmt.Println("✅ Connected to Redis successfully")

	// Create the Redis repository
	repo := adapters.NewRedisRepository(redisClient)

	// Create custom configuration for faster demo
	config := service.Config{
		FailureThreshold: 3,               // Open after 3 failures
		SuccessThreshold: 2,               // Close after 2 successes in half-open
		Timeout:          2 * time.Second, // 2 second timeout
		CooldownPeriod:   3 * time.Second, // 3 second cooldown
	}

	// Create the circuit breaker service
	breakerService := service.NewCircuitBreakerServiceWithConfig(repo, config)

	fmt.Printf("📋 Circuit Breaker Configuration:\n")
	fmt.Printf("   • Failure Threshold: %d\n", config.FailureThreshold)
	fmt.Printf("   • Success Threshold: %d\n", config.SuccessThreshold)
	fmt.Printf("   • Timeout: %v\n", config.Timeout)
	fmt.Printf("   • Cooldown Period: %v\n\n", config.CooldownPeriod)

	// Run different demo scenarios
	runBasicFailureScenario(breakerService)
	fmt.Println()
	runRecoveryScenario(breakerService)
	fmt.Println()
	runTimeoutScenario(breakerService)
	fmt.Println()
	runMultipleServicesScenario(breakerService)
	fmt.Println()
	runManualControlScenario(breakerService)

	fmt.Println("🎉 Circuit Breaker Demo Complete!")
}

// Scenario 1: Basic failure and circuit opening
func runBasicFailureScenario(breakerService *service.CircuitBreakerService) {
	fmt.Println("📍 SCENARIO 1: Basic Failure Scenario")
	fmt.Println("=====================================")

	serviceID := "user-service"

	// Simulate consecutive failures to open the circuit
	for i := 1; i <= 6; i++ {
		err := breakerService.Execute(serviceID, func() error {
			if i <= 4 { // First 4 attempts fail
				return errors.New("service unavailable")
			}
			return nil // Later attempts would succeed
		})

		// Get current state and stats
		state, _ := breakerService.GetState(serviceID)
		stats, _ := breakerService.GetStats(serviceID)

		fmt.Printf("Attempt %d: ", i)
		if err != nil {
			if err == core.ErrCircuitOpen {
				fmt.Printf("❌ BLOCKED (Circuit OPEN) - %v\n", err)
			} else {
				fmt.Printf("❌ FAILED - %v\n", err)
			}
		} else {
			fmt.Printf("✅ SUCCESS\n")
		}

		fmt.Printf("         State: %s, Failures: %d, Successes: %d\n",
			state.String(), stats.FailureCount, stats.SuccessCount)

		time.Sleep(500 * time.Millisecond)
	}
}

// Scenario 2: Circuit recovery from half-open to closed
func runRecoveryScenario(breakerService *service.CircuitBreakerService) {
	fmt.Println("📍 SCENARIO 2: Circuit Recovery Scenario")
	fmt.Println("=======================================")

	serviceID := "payment-service"

	// First, force the circuit open by creating failures
	for i := 1; i <= 3; i++ {
		breakerService.Execute(serviceID, func() error {
			return errors.New("payment gateway down")
		})
	}

	fmt.Println("🔴 Circuit is now OPEN due to failures")
	state, _ := breakerService.GetState(serviceID)
	fmt.Printf("   Current State: %s\n", state.String())

	// Wait for cooldown period
	fmt.Println("⏳ Waiting for cooldown period (3 seconds)...")
	time.Sleep(3500 * time.Millisecond)

	// Now simulate recovery
	for i := 1; i <= 4; i++ {
		err := breakerService.Execute(serviceID, func() error {
			// Simulate service recovery - all requests now succeed
			return nil
		})

		state, _ := breakerService.GetState(serviceID)
		stats, _ := breakerService.GetStats(serviceID)

		fmt.Printf("Recovery Attempt %d: ", i)
		if err != nil {
			fmt.Printf("❌ FAILED - %v\n", err)
		} else {
			fmt.Printf("✅ SUCCESS\n")
		}

		fmt.Printf("                   State: %s, Failures: %d, Successes: %d\n",
			state.String(), stats.FailureCount, stats.SuccessCount)

		time.Sleep(500 * time.Millisecond)
	}
}

// Scenario 3: Timeout handling
func runTimeoutScenario(breakerService *service.CircuitBreakerService) {
	fmt.Println("📍 SCENARIO 3: Timeout Scenario")
	fmt.Println("===============================")

	serviceID := "slow-service"

	for i := 1; i <= 3; i++ {
		start := time.Now()
		err := breakerService.Execute(serviceID, func() error {
			// Simulate slow service (longer than 2 second timeout)
			time.Sleep(3 * time.Second)
			return nil
		})
		duration := time.Since(start)

		state, _ := breakerService.GetState(serviceID)

		fmt.Printf("Timeout Test %d: ", i)
		if err != nil {
			if err == core.ErrTimeout {
				fmt.Printf("⏰ TIMEOUT after %v\n", duration.Round(time.Millisecond))
			} else {
				fmt.Printf("❌ ERROR - %v\n", err)
			}
		} else {
			fmt.Printf("✅ SUCCESS in %v\n", duration.Round(time.Millisecond))
		}

		fmt.Printf("               State: %s\n", state.String())
	}
}

// Scenario 4: Multiple services with different behaviors
func runMultipleServicesScenario(breakerService *service.CircuitBreakerService) {
	fmt.Println("📍 SCENARIO 4: Multiple Services Scenario")
	fmt.Println("=========================================")

	services := []struct {
		name        string
		failureRate float64 // 0.0 to 1.0
	}{
		{"database-service", 0.8}, // 80% failure rate
		{"cache-service", 0.2},    // 20% failure rate
		{"email-service", 0.0},    // 0% failure rate (always works)
	}

	rand.Seed(time.Now().UnixNano())

	for round := 1; round <= 3; round++ {
		fmt.Printf("\n--- Round %d ---\n", round)

		for _, svc := range services {
			err := breakerService.Execute(svc.name, func() error {
				if rand.Float64() < svc.failureRate {
					return fmt.Errorf("%s temporarily unavailable", svc.name)
				}
				return nil
			})

			state, _ := breakerService.GetState(svc.name)
			stats, _ := breakerService.GetStats(svc.name)

			status := "✅ SUCCESS"
			if err != nil {
				if err == core.ErrCircuitOpen {
					status = "🚫 BLOCKED"
				} else {
					status = "❌ FAILED"
				}
			}

			fmt.Printf("%-20s: %s (State: %s, F:%d, S:%d)\n",
				svc.name, status, state.String(), stats.FailureCount, stats.SuccessCount)
		}

		time.Sleep(1 * time.Second)
	}

	// Show final summary
	fmt.Println("\n📊 Final Service States:")
	for _, svc := range services {
		stats, _ := breakerService.GetStats(svc.name)
		fmt.Printf("%-20s: %s (Failures: %d, Successes: %d)\n",
			svc.name, stats.State.String(), stats.FailureCount, stats.SuccessCount)
	}
}

// Scenario 5: Manual control (force open/close)
func runManualControlScenario(breakerService *service.CircuitBreakerService) {
	fmt.Println("📍 SCENARIO 5: Manual Control Scenario")
	fmt.Println("======================================")

	serviceID := "admin-service"

	// Create a working service
	err := breakerService.Execute(serviceID, func() error { return nil })
	fmt.Printf("Initial execution: %s\n", getResultString(err))

	state, _ := breakerService.GetState(serviceID)
	fmt.Printf("Initial state: %s\n", state.String())

	// Manually force open
	fmt.Println("\n🔧 Manually forcing circuit OPEN...")
	breakerService.ForceOpen(serviceID)

	state, _ = breakerService.GetState(serviceID)
	fmt.Printf("After ForceOpen: %s\n", state.String())

	// Try to execute (should be blocked)
	err = breakerService.Execute(serviceID, func() error { return nil })
	fmt.Printf("Execution attempt: %s\n", getResultString(err))

	// Manually force close
	fmt.Println("\n🔧 Manually forcing circuit CLOSED...")
	breakerService.ForceClose(serviceID)

	state, _ = breakerService.GetState(serviceID)
	fmt.Printf("After ForceClose: %s\n", state.String())

	// Try to execute (should work)
	err = breakerService.Execute(serviceID, func() error { return nil })
	fmt.Printf("Execution attempt: %s\n", getResultString(err))

	// Show all circuit breakers
	fmt.Println("\n📋 All Circuit Breakers:")
	ids, _ := breakerService.ListCircuitBreakers()
	for _, id := range ids {
		state, _ := breakerService.GetState(id)
		stats, _ := breakerService.GetStats(id)
		fmt.Printf("  %s: %s (F:%d, S:%d)\n",
			id, state.String(), stats.FailureCount, stats.SuccessCount)
	}
}

// Helper function to format execution results
func getResultString(err error) string {
	if err == nil {
		return "✅ SUCCESS"
	}
	if err == core.ErrCircuitOpen {
		return "🚫 BLOCKED (Circuit Open)"
	}
	if err == core.ErrTimeout {
		return "⏰ TIMEOUT"
	}
	return fmt.Sprintf("❌ FAILED (%v)", err)
}
