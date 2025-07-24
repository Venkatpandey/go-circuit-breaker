package service

import (
	"context"
	"errors"
	"fmt"
	"go-circuit-breaker/core"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Mock repository for testing
type mockRepository struct {
	mu          sync.RWMutex
	data        map[string]*core.CircuitBreaker
	findError   error
	saveError   error
	deleteError error
	listError   error
	findCalls   int
	saveCalls   int
	deleteCalls int
	listCalls   int
}

func newMockRepository() *mockRepository {
	return &mockRepository{
		data: make(map[string]*core.CircuitBreaker),
	}
}

func (m *mockRepository) FindByID(id string) (*core.CircuitBreaker, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.findCalls++

	if m.findError != nil {
		return nil, m.findError
	}

	cb, exists := m.data[id]
	if !exists {
		return nil, nil
	}
	return cb, nil
}

func (m *mockRepository) Save(id string, cb *core.CircuitBreaker) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++

	if m.saveError != nil {
		return m.saveError
	}

	m.data[id] = cb
	return nil
}

func (m *mockRepository) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls++

	if m.deleteError != nil {
		return m.deleteError
	}

	delete(m.data, id)
	return nil
}

func (m *mockRepository) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.listCalls++

	if m.listError != nil {
		return nil, m.listError
	}

	var ids []string
	for id := range m.data {
		ids = append(ids, id)
	}
	return ids, nil
}

func (m *mockRepository) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string]*core.CircuitBreaker)
	m.findError = nil
	m.saveError = nil
	m.deleteError = nil
	m.listError = nil
	m.findCalls = 0
	m.saveCalls = 0
	m.deleteCalls = 0
	m.listCalls = 0
}

func TestNewCircuitBreakerService(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	if service.repo != repo {
		t.Errorf("expected repository to be set")
	}

	if service.cache == nil {
		t.Errorf("expected cache to be initialized")
	}

	expectedConfig := DefaultConfig()
	if service.config != expectedConfig {
		t.Errorf("expected default config, got %+v", service.config)
	}
}

func TestNewCircuitBreakerServiceWithConfig(t *testing.T) {
	repo := newMockRepository()
	customConfig := Config{
		FailureThreshold: 10,
		SuccessThreshold: 5,
		Timeout:          2 * time.Second,
		CooldownPeriod:   10 * time.Second,
	}

	service := NewCircuitBreakerServiceWithConfig(repo, customConfig)

	if service.config != customConfig {
		t.Errorf("expected custom config %+v, got %+v", customConfig, service.config)
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.FailureThreshold != 3 {
		t.Errorf("expected failure threshold 3, got %d", config.FailureThreshold)
	}
	if config.SuccessThreshold != 2 {
		t.Errorf("expected success threshold 2, got %d", config.SuccessThreshold)
	}
	if config.Timeout != time.Second {
		t.Errorf("expected timeout 1s, got %v", config.Timeout)
	}
	if config.CooldownPeriod != 5*time.Second {
		t.Errorf("expected cooldown 5s, got %v", config.CooldownPeriod)
	}
}

func TestExecuteSuccess(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	var executed bool
	err := service.Execute("test-id", func() error {
		executed = true
		return nil
	})

	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !executed {
		t.Errorf("expected function to be executed")
	}

	if repo.saveCalls != 2 { // Once for creation, once for execution
		t.Errorf("expected 2 save calls, got %d", repo.saveCalls)
	}
}

func TestExecuteFailure(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	expectedError := errors.New("test error")
	err := service.Execute("test-id", func() error {
		return expectedError
	})

	if err != expectedError {
		t.Errorf("expected error %v, got %v", expectedError, err)
	}
}

func TestExecuteWithContext(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := service.ExecuteWithContext(ctx, "test-id", func() error {
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	if err != core.ErrTimeout {
		t.Errorf("expected timeout error, got %v", err)
	}
}

func TestExecuteEmptyID(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	err := service.Execute("", func() error {
		return nil
	})

	if err == nil {
		t.Errorf("expected error for empty ID")
	}
}

func TestExecuteRepositoryFindError(t *testing.T) {
	repo := newMockRepository()
	repo.findError = errors.New("repository error")
	service := NewCircuitBreakerService(repo)

	err := service.Execute("test-id", func() error {
		return nil
	})

	if err == nil {
		t.Errorf("expected error from repository")
	}
}

func TestExecuteSaveError(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// First execution to create the circuit breaker
	service.Execute("test-id", func() error { return nil })

	// Set save error for subsequent calls
	repo.saveError = errors.New("save error")

	err := service.Execute("test-id", func() error {
		return nil
	})

	// Should not return save error, but should log it
	if err != nil {
		t.Errorf("expected no error despite save failure, got %v", err)
	}
}

func TestGetStats(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// Execute some operations to generate stats
	service.Execute("test-id", func() error { return nil })
	service.Execute("test-id", func() error { return errors.New("test error") })

	stats, err := service.GetStats("test-id")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if stats.State != core.StateClosed {
		t.Errorf("expected state CLOSED, got %v", stats.State)
	}
	if stats.SuccessCount != 1 {
		t.Errorf("expected success count 1, got %d", stats.SuccessCount)
	}
}

func TestGetStatsEmptyID(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	_, err := service.GetStats("")
	if err == nil {
		t.Errorf("expected error for empty ID")
	}
}

func TestGetState(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	state, err := service.GetState("test-id")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if state != core.StateClosed {
		t.Errorf("expected state CLOSED, got %v", state)
	}
}

func TestForceOpen(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	err := service.ForceOpen("test-id")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	state, _ := service.GetState("test-id")
	if state != core.StateOpen {
		t.Errorf("expected state OPEN, got %v", state)
	}

	// Verify that execution is blocked
	execErr := service.Execute("test-id", func() error {
		return nil
	})
	if execErr != core.ErrCircuitOpen {
		t.Errorf("expected circuit open error, got %v", execErr)
	}
}

func TestForceClose(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// First force open
	service.ForceOpen("test-id")

	// Then force close
	err := service.ForceClose("test-id")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	state, _ := service.GetState("test-id")
	if state != core.StateClosed {
		t.Errorf("expected state CLOSED, got %v", state)
	}

	// Verify that execution works
	execErr := service.Execute("test-id", func() error {
		return nil
	})
	if execErr != nil {
		t.Errorf("expected no error after force close, got %v", execErr)
	}
}

func TestCreateCircuitBreaker(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	config := Config{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		Timeout:          2 * time.Second,
		CooldownPeriod:   10 * time.Second,
	}

	err := service.CreateCircuitBreaker("custom-id", config)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify it exists in cache
	service.mu.RLock()
	_, exists := service.cache["custom-id"]
	service.mu.RUnlock()

	if !exists {
		t.Errorf("expected circuit breaker to be cached")
	}

	// Verify it was saved to repository
	if repo.saveCalls != 1 {
		t.Errorf("expected 1 save call, got %d", repo.saveCalls)
	}
}

func TestCreateCircuitBreakerEmptyID(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	config := DefaultConfig()
	err := service.CreateCircuitBreaker("", config)

	if err == nil {
		t.Errorf("expected error for empty ID")
	}
}

func TestCreateCircuitBreakerInvalidConfig(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	config := Config{
		FailureThreshold: 0, // Invalid
		SuccessThreshold: 2,
		Timeout:          time.Second,
		CooldownPeriod:   5 * time.Second,
	}

	err := service.CreateCircuitBreaker("test-id", config)
	if err == nil {
		t.Errorf("expected error for invalid config")
	}
}

func TestDeleteCircuitBreaker(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// Create a circuit breaker first
	service.CreateCircuitBreaker("test-id", DefaultConfig())

	err := service.DeleteCircuitBreaker("test-id")
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify it's removed from cache
	service.mu.RLock()
	_, exists := service.cache["test-id"]
	service.mu.RUnlock()

	if exists {
		t.Errorf("expected circuit breaker to be removed from cache")
	}

	// Verify delete was called on repository
	if repo.deleteCalls != 1 {
		t.Errorf("expected 1 delete call, got %d", repo.deleteCalls)
	}
}

func TestDeleteCircuitBreakerRepositoryError(t *testing.T) {
	repo := newMockRepository()
	repo.deleteError = errors.New("delete error")
	service := NewCircuitBreakerService(repo)

	err := service.DeleteCircuitBreaker("test-id")
	if err == nil {
		t.Errorf("expected error from repository")
	}
}

func TestListCircuitBreakers(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// Create some circuit breakers
	service.CreateCircuitBreaker("id1", DefaultConfig())
	service.CreateCircuitBreaker("id2", DefaultConfig())

	ids, err := service.ListCircuitBreakers()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if len(ids) != 2 {
		t.Errorf("expected 2 IDs, got %d", len(ids))
	}

	expectedIDs := map[string]bool{"id1": true, "id2": true}
	for _, id := range ids {
		if !expectedIDs[id] {
			t.Errorf("unexpected ID: %s", id)
		}
	}
}

func TestListCircuitBreakersRepositoryError(t *testing.T) {
	repo := newMockRepository()
	repo.listError = errors.New("list error")
	service := NewCircuitBreakerService(repo)

	_, err := service.ListCircuitBreakers()
	if err == nil {
		t.Errorf("expected error from repository")
	}
}

func TestCaching(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// First execution should create and cache the circuit breaker
	service.Execute("test-id", func() error { return nil })
	initialFindCalls := repo.findCalls

	// Second execution should use cached version
	service.Execute("test-id", func() error { return nil })

	if repo.findCalls != initialFindCalls {
		t.Errorf("expected cached circuit breaker to be used, but repository was called again")
	}
}

func TestClearCache(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// Create some circuit breakers
	service.Execute("id1", func() error { return nil })
	service.Execute("id2", func() error { return nil })

	// Verify cache has entries
	service.mu.RLock()
	cacheSize := len(service.cache)
	service.mu.RUnlock()

	if cacheSize != 2 {
		t.Errorf("expected 2 cached entries, got %d", cacheSize)
	}

	// Clear cache
	service.ClearCache()

	// Verify cache is empty
	service.mu.RLock()
	cacheSize = len(service.cache)
	service.mu.RUnlock()

	if cacheSize != 0 {
		t.Errorf("expected empty cache, got %d entries", cacheSize)
	}
}

func TestUpdateConfig(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	newConfig := Config{
		FailureThreshold: 10,
		SuccessThreshold: 5,
		Timeout:          3 * time.Second,
		CooldownPeriod:   15 * time.Second,
	}

	service.UpdateConfig(newConfig)

	currentConfig := service.GetConfig()
	if !reflect.DeepEqual(currentConfig, newConfig) {
		t.Errorf("expected config %+v, got %+v", newConfig, currentConfig)
	}
}

func TestGetConfig(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	config := service.GetConfig()
	expectedConfig := DefaultConfig()

	if !reflect.DeepEqual(config, expectedConfig) {
		t.Errorf("expected config %+v, got %+v", expectedConfig, config)
	}
}

func TestHealthCheck(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	err := service.HealthCheck()
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if repo.listCalls != 1 {
		t.Errorf("expected 1 list call for health check, got %d", repo.listCalls)
	}
}

func TestHealthCheckRepositoryError(t *testing.T) {
	repo := newMockRepository()
	repo.listError = errors.New("repository down")
	service := NewCircuitBreakerService(repo)

	err := service.HealthCheck()
	if err == nil {
		t.Errorf("expected error from health check")
	}
}

func TestConcurrentAccess(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 10

	// Run concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			cbID := fmt.Sprintf("cb-%d", id%5) // Use 5 different circuit breakers

			for j := 0; j < numOperations; j++ {
				service.Execute(cbID, func() error {
					if j%3 == 0 {
						return errors.New("test error")
					}
					return nil
				})
			}
		}(i)
	}

	wg.Wait()

	// Verify service is still functional
	err := service.Execute("test-concurrent", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("service should still be functional after concurrent access, got error: %v", err)
	}
}

func TestExistingCircuitBreakerFromRepository(t *testing.T) {
	repo := newMockRepository()

	// Pre-populate repository with a circuit breaker
	existingCB, _ := core.NewCircuitBreaker(5, 3, 2*time.Second, 10*time.Second)
	repo.data["existing-id"] = existingCB

	service := NewCircuitBreakerService(repo)

	// Execute should use the existing circuit breaker
	err := service.Execute("existing-id", func() error {
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Verify it was loaded from repository and cached
	service.mu.RLock()
	cached, exists := service.cache["existing-id"]
	service.mu.RUnlock()

	if !exists {
		t.Errorf("expected circuit breaker to be cached")
	}
	if cached != existingCB {
		t.Errorf("expected cached circuit breaker to be the same instance")
	}
}

// Benchmark tests
func BenchmarkServiceExecute(b *testing.B) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			service.Execute("bench-test", func() error {
				i++
				return nil
			})
		}
	})
}

func BenchmarkServiceExecuteWithCaching(b *testing.B) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	// Pre-warm the cache
	service.Execute("bench-test", func() error { return nil })

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			service.Execute("bench-test", func() error {
				return nil
			})
		}
	})
}
