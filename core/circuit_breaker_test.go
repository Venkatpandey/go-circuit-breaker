package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestNewCircuitBreaker(t *testing.T) {
	tests := []struct {
		name             string
		failureThreshold int
		successThreshold int
		timeout          time.Duration
		cooldownPeriod   time.Duration
		expectError      bool
	}{
		{
			name:             "valid configuration",
			failureThreshold: 3,
			successThreshold: 2,
			timeout:          time.Second,
			cooldownPeriod:   5 * time.Second,
			expectError:      false,
		},
		{
			name:             "zero failure threshold",
			failureThreshold: 0,
			successThreshold: 2,
			timeout:          time.Second,
			cooldownPeriod:   5 * time.Second,
			expectError:      true,
		},
		{
			name:             "negative failure threshold",
			failureThreshold: -1,
			successThreshold: 2,
			timeout:          time.Second,
			cooldownPeriod:   5 * time.Second,
			expectError:      true,
		},
		{
			name:             "zero success threshold",
			failureThreshold: 3,
			successThreshold: 0,
			timeout:          time.Second,
			cooldownPeriod:   5 * time.Second,
			expectError:      true,
		},
		{
			name:             "zero timeout",
			failureThreshold: 3,
			successThreshold: 2,
			timeout:          0,
			cooldownPeriod:   5 * time.Second,
			expectError:      true,
		},
		{
			name:             "zero cooldown period",
			failureThreshold: 3,
			successThreshold: 2,
			timeout:          time.Second,
			cooldownPeriod:   0,
			expectError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb, err := NewCircuitBreaker(tt.failureThreshold, tt.successThreshold, tt.timeout, tt.cooldownPeriod)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if cb != nil {
					t.Errorf("expected nil circuit breaker but got %v", cb)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if cb == nil {
					t.Errorf("expected circuit breaker but got nil")
				}
				if cb.GetState() != StateClosed {
					t.Errorf("expected initial state to be CLOSED, got %v", cb.GetState())
				}
			}
		})
	}
}

func TestCircuitBreakerClosedState(t *testing.T) {
	cb, err := NewCircuitBreaker(3, 2, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Test successful executions
	for i := 0; i < 5; i++ {
		err := cb.Execute(func() error {
			return nil
		})
		if err != nil {
			t.Errorf("unexpected error on success: %v", err)
		}
		if cb.GetState() != StateClosed {
			t.Errorf("expected state CLOSED, got %v", cb.GetState())
		}
	}

	// Test failures without reaching threshold
	for i := 0; i < 2; i++ {
		err := cb.Execute(func() error {
			return errors.New("test error")
		})
		if err == nil {
			t.Errorf("expected error but got none")
		}
		if cb.GetState() != StateClosed {
			t.Errorf("expected state CLOSED, got %v", cb.GetState())
		}
	}

	// Test failure that should open the circuit
	err = cb.Execute(func() error {
		return errors.New("test error")
	})
	if err == nil {
		t.Errorf("expected error but got none")
	}
	if cb.GetState() != StateOpen {
		t.Errorf("expected state OPEN, got %v", cb.GetState())
	}
}

func TestCircuitBreakerOpenState(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Force circuit to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	if cb.GetState() != StateOpen {
		t.Errorf("expected state OPEN, got %v", cb.GetState())
	}

	// Test that requests are rejected while open
	err = cb.Execute(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for cooldown period
	time.Sleep(150 * time.Millisecond)

	// Next request should transition to half-open
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error after cooldown: %v", err)
	}
	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state HALF_OPEN, got %v", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenState(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Force circuit to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Execute successful request to enter half-open
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state HALF_OPEN, got %v", cb.GetState())
	}

	// Test successful recovery (half-open -> closed)
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cb.GetState() != StateClosed {
		t.Errorf("expected state CLOSED after successful recovery, got %v", cb.GetState())
	}
}

func TestCircuitBreakerHalfOpenFailure(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Force circuit to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	// Wait for cooldown
	time.Sleep(60 * time.Millisecond)

	// Execute successful request to enter half-open
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if cb.GetState() != StateHalfOpen {
		t.Errorf("expected state HALF_OPEN, got %v", cb.GetState())
	}

	// Test failure in half-open should immediately go back to open
	err = cb.Execute(func() error {
		return errors.New("test error")
	})
	if err == nil {
		t.Errorf("expected error but got none")
	}
	if cb.GetState() != StateOpen {
		t.Errorf("expected state OPEN after half-open failure, got %v", cb.GetState())
	}
}

func TestCircuitBreakerTimeout(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, 100*time.Millisecond, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Test timeout
	start := time.Now()
	err = cb.Execute(func() error {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	duration := time.Since(start)

	if err != ErrTimeout {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
	if duration >= 200*time.Millisecond {
		t.Errorf("expected timeout to prevent long execution, took %v", duration)
	}
}

func TestCircuitBreakerContextTimeout(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err = cb.ExecuteWithContext(ctx, func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})
	duration := time.Since(start)

	if err != ErrTimeout {
		t.Errorf("expected ErrTimeout, got %v", err)
	}
	if duration >= 100*time.Millisecond {
		t.Errorf("expected context timeout to prevent long execution, took %v", duration)
	}
}

func TestCircuitBreakerConcurrency(t *testing.T) {
	cb, err := NewCircuitBreaker(10, 5, time.Second, time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	var wg sync.WaitGroup
	var successCount, errorCount int64
	var mu sync.Mutex

	// Run concurrent operations
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			err := cb.Execute(func() error {
				if index%3 == 0 {
					return errors.New("test error")
				}
				return nil
			})

			mu.Lock()
			if err != nil {
				errorCount++
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	mu.Lock()
	total := successCount + errorCount
	mu.Unlock()

	if total != 100 {
		t.Errorf("expected 100 total operations, got %d", total)
	}

	// Verify circuit breaker still works after concurrent access
	stats := cb.GetStats()
	if stats.State < StateClosed || stats.State > StateHalfOpen {
		t.Errorf("invalid state after concurrent operations: %v", stats.State)
	}
}

func TestCircuitBreakerStats(t *testing.T) {
	cb, err := NewCircuitBreaker(3, 2, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Initial stats
	stats := cb.GetStats()
	if stats.State != StateClosed {
		t.Errorf("expected initial state CLOSED, got %v", stats.State)
	}
	if stats.FailureCount != 0 {
		t.Errorf("expected initial failure count 0, got %d", stats.FailureCount)
	}
	if stats.SuccessCount != 0 {
		t.Errorf("expected initial success count 0, got %d", stats.SuccessCount)
	}

	// Execute some operations
	cb.Execute(func() error { return nil })
	cb.Execute(func() error { return errors.New("test") })

	stats = cb.GetStats()
	if stats.SuccessCount != 1 {
		t.Errorf("expected success count 1, got %d", stats.SuccessCount)
	}
	if stats.FailureCount != 1 {
		t.Errorf("expected failure count 1, got %d", stats.FailureCount)
	}
}

func TestCircuitBreakerForceOpen(t *testing.T) {
	cb, err := NewCircuitBreaker(3, 2, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	cb.ForceOpen()

	if cb.GetState() != StateOpen {
		t.Errorf("expected state OPEN after ForceOpen, got %v", cb.GetState())
	}

	err = cb.Execute(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("expected ErrCircuitOpen after ForceOpen, got %v", err)
	}
}

func TestCircuitBreakerForceClose(t *testing.T) {
	cb, err := NewCircuitBreaker(2, 2, time.Second, 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Force circuit to open
	for i := 0; i < 2; i++ {
		cb.Execute(func() error {
			return errors.New("test error")
		})
	}

	if cb.GetState() != StateOpen {
		t.Errorf("expected state OPEN, got %v", cb.GetState())
	}

	cb.ForceClose()

	if cb.GetState() != StateClosed {
		t.Errorf("expected state CLOSED after ForceClose, got %v", cb.GetState())
	}

	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error after ForceClose: %v", err)
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "CLOSED"},
		{StateOpen, "OPEN"},
		{StateHalfOpen, "HALF_OPEN"},
		{State(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		if tt.state.String() != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, tt.state.String())
		}
	}
}

func TestCircuitBreakerFailureRecovery(t *testing.T) {
	cb, err := NewCircuitBreaker(3, 2, time.Second, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("failed to create circuit breaker: %v", err)
	}

	// Test that success resets failure count in closed state
	cb.Execute(func() error { return errors.New("error") })
	cb.Execute(func() error { return errors.New("error") })

	stats := cb.GetStats()
	if stats.FailureCount != 2 {
		t.Errorf("expected failure count 2, got %d", stats.FailureCount)
	}

	// Success should reset failure count
	cb.Execute(func() error { return nil })

	stats = cb.GetStats()
	if stats.FailureCount != 0 {
		t.Errorf("expected failure count reset to 0 after success, got %d", stats.FailureCount)
	}
}

// Benchmark tests
func BenchmarkCircuitBreakerExecute(b *testing.B) {
	cb, _ := NewCircuitBreaker(10, 5, time.Second, 5*time.Second)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb.Execute(func() error {
				return nil
			})
		}
	})
}

func BenchmarkCircuitBreakerExecuteWithFailures(b *testing.B) {
	cb, _ := NewCircuitBreaker(1000, 5, time.Second, 5*time.Second)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			cb.Execute(func() error {
				i++
				if i%10 == 0 {
					return errors.New("test error")
				}
				return nil
			})
		}
	})
}
