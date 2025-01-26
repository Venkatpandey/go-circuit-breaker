package adapters

import (
	"go-circuit-breaker/core"
	"testing"
	"time"
)

func TestMemoryRepository_SaveAndFindByID(t *testing.T) {
	repo := NewMemoryRepository()

	cb := core.NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)
	cb.State = core.StateOpen

	// Save the circuit breaker
	err := repo.Save(cb)
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
