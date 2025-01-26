package service

import (
	"errors"
	"go-circuit-breaker/adapters"
	"testing"
)

func TestCircuitBreakerService_Execute(t *testing.T) {
	repo := adapters.NewMemoryRepository()
	service := NewCircuitBreakerService(repo)

	// Test successful execution
	err := service.Execute("default", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test failure execution
	err = service.Execute("default", func() error {
		return errors.New("service error")
	})
	if err == nil {
		t.Error("Expected an error, got nil")
	}

	// Simulate failures to trip the circuit breaker
	for i := 0; i < 3; i++ {
		_ = service.Execute("default", func() error {
			return errors.New("service error")
		})
	}

	// Circuit should be open now
	err = service.Execute("default", func() error {
		return nil
	})
	if err == nil {
		t.Error("Expected an error, got nil")
	}
}
