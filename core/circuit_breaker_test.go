package core

import (
	"errors"
	"testing"
	"time"
)

func TestCircuitBreaker_Execute_ClosedState(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)

	// Test successful execution
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test failure execution
	err = cb.Execute(func() error {
		return errors.New("service error")
	})
	if err == nil {
		t.Error("Expected an error, got nil")
	}
}

func TestCircuitBreaker_Execute_OpenState(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)

	// Simulate failures to trip the circuit breaker
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return errors.New("service error")
		})
	}

	// Circuit should be open now
	err := cb.Execute(func() error {
		return nil
	})
	if err != ErrCircuitOpen {
		t.Errorf("Expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_Execute_HalfOpenState(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)

	// Simulate failures to trip the circuit breaker
	for i := 0; i < 3; i++ {
		_ = cb.Execute(func() error {
			return errors.New("service error")
		})
	}

	// Wait for cooldown period
	time.Sleep(6 * time.Second)

	// Circuit should be half-open now
	err := cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Verify state transition to closed after success threshold
	err = cb.Execute(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cb.State != StateClosed {
		t.Errorf("Expected state to be Closed, got %v", cb.State)
	}
}
