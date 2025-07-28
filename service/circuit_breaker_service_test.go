package service

import (
	"context"
	"errors"
	"fmt"
	"go-circuit-breaker/core"
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
	tests := []struct {
		name           string
		customConfig   *Config
		expectedConfig Config
	}{
		{
			name:           "default config",
			customConfig:   nil,
			expectedConfig: DefaultConfig(),
		},
		{
			name: "custom config",
			customConfig: &Config{
				FailureThreshold: 10,
				SuccessThreshold: 5,
				Timeout:          2 * time.Second,
				CooldownPeriod:   10 * time.Second,
			},
			expectedConfig: Config{
				FailureThreshold: 10,
				SuccessThreshold: 5,
				Timeout:          2 * time.Second,
				CooldownPeriod:   10 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			var service *CircuitBreakerService

			if tt.customConfig != nil {
				service = NewCircuitBreakerServiceWithConfig(repo, *tt.customConfig)
			} else {
				service = NewCircuitBreakerService(repo)
			}

			if service.repo != repo {
				t.Errorf("expected repository to be set")
			}
			if service.cache == nil {
				t.Errorf("expected cache to be initialized")
			}
			if service.config != tt.expectedConfig {
				t.Errorf("expected config %+v, got %+v", tt.expectedConfig, service.config)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name     string
		actual   interface{}
		expected interface{}
	}{
		{"failure threshold", config.FailureThreshold, 3},
		{"success threshold", config.SuccessThreshold, 2},
		{"timeout", config.Timeout, time.Second},
		{"cooldown period", config.CooldownPeriod, 5 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.actual != tt.expected {
				t.Errorf("expected %s %v, got %v", tt.name, tt.expected, tt.actual)
			}
		})
	}
}

func TestExecute(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		fn            func() error
		setupRepo     func(*mockRepository)
		expectError   bool
		expectedError error
		expectedCalls int
		shouldExecute bool
	}{
		{
			name: "success",
			id:   "test-id",
			fn: func() error {
				return nil
			},
			expectedCalls: 2, // Create + Execute
			shouldExecute: true,
		},
		{
			name: "function error",
			id:   "test-id",
			fn: func() error {
				return errors.New("test error")
			},
			expectError:   true,
			expectedError: errors.New("test error"),
			expectedCalls: 2,
			shouldExecute: true,
		},
		{
			name: "empty id",
			id:   "",
			fn: func() error {
				return nil
			},
			expectError: true,
		},
		{
			name: "repository find error",
			id:   "test-id",
			fn: func() error {
				return nil
			},
			setupRepo: func(repo *mockRepository) {
				repo.findError = errors.New("repository error")
			},
			expectError: true,
		},
		{
			name: "save error does not affect execution",
			id:   "test-id",
			fn: func() error {
				return nil
			},
			setupRepo: func(repo *mockRepository) {
				// First execution to create the circuit breaker
				service := NewCircuitBreakerService(repo)
				service.Execute("test-id", func() error { return nil })
				repo.saveError = errors.New("save error")
			},
			expectedCalls: 3, // Only the second call
			shouldExecute: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			if tt.setupRepo != nil {
				tt.setupRepo(repo)
			}

			executed := false
			testFn := func() error {
				executed = true
				return tt.fn()
			}

			err := service.Execute(tt.id, testFn)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if tt.expectedError != nil && err.Error() != tt.expectedError.Error() {
				t.Errorf("expected error %v, got %v", tt.expectedError, err)
			}
			if tt.shouldExecute && !executed {
				t.Errorf("expected function to be executed")
			}
			if tt.expectedCalls > 0 && repo.saveCalls != tt.expectedCalls {
				t.Errorf("expected %d save calls, got %d", tt.expectedCalls, repo.saveCalls)
			}
		})
	}
}

func TestExecuteWithContext(t *testing.T) {
	tests := []struct {
		name          string
		timeout       time.Duration
		fnDuration    time.Duration
		expectTimeout bool
	}{
		{
			name:          "successful execution within timeout",
			timeout:       100 * time.Millisecond,
			fnDuration:    10 * time.Millisecond,
			expectTimeout: false,
		},
		{
			name:          "timeout exceeded",
			timeout:       50 * time.Millisecond,
			fnDuration:    100 * time.Millisecond,
			expectTimeout: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			err := service.ExecuteWithContext(ctx, "test-id", func() error {
				time.Sleep(tt.fnDuration)
				return nil
			})

			if tt.expectTimeout && err != core.ErrTimeout {
				t.Errorf("expected timeout error, got %v", err)
			}
			if !tt.expectTimeout && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

func TestStateOperations(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		operation     string
		expectError   bool
		expectedState core.State
	}{
		{
			name:          "get initial state",
			id:            "test-id",
			operation:     "getState",
			expectedState: core.StateClosed,
		},
		{
			name:          "force open",
			id:            "test-id",
			operation:     "forceOpen",
			expectedState: core.StateOpen,
		},
		{
			name:          "force close",
			id:            "test-id",
			operation:     "forceClose",
			expectedState: core.StateClosed,
		},
		{
			name:        "empty id",
			id:          "",
			operation:   "getState",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			var err error
			switch tt.operation {
			case "getState":
				_, err = service.GetState(tt.id)
			case "forceOpen":
				err = service.ForceOpen(tt.id)
			case "forceClose":
				err = service.ForceClose(tt.id)
			}

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}

			if !tt.expectError && tt.expectedState.String() != "" {
				state, _ := service.GetState(tt.id)
				if state != tt.expectedState {
					t.Errorf("expected state %v, got %v", tt.expectedState, state)
				}
			}
		})
	}
}

func TestCreateCircuitBreaker(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		config      Config
		expectError bool
	}{
		{
			name:   "valid creation",
			id:     "test-id",
			config: DefaultConfig(),
		},
		{
			name:        "empty id",
			id:          "",
			config:      DefaultConfig(),
			expectError: true,
		},
		{
			name: "invalid config - zero failure threshold",
			id:   "test-id",
			config: Config{
				FailureThreshold: 0,
				SuccessThreshold: 2,
				Timeout:          time.Second,
				CooldownPeriod:   5 * time.Second,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			err := service.CreateCircuitBreaker(tt.id, tt.config)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}

			if !tt.expectError {
				service.mu.RLock()
				_, exists := service.cache[tt.id]
				service.mu.RUnlock()

				if !exists {
					t.Errorf("expected circuit breaker to be cached")
				}
				if repo.saveCalls != 1 {
					t.Errorf("expected 1 save call, got %d", repo.saveCalls)
				}
			}
		})
	}
}

func TestDeleteCircuitBreaker(t *testing.T) {
	tests := []struct {
		name        string
		setupRepo   func(*mockRepository)
		id          string
		expectError bool
	}{
		{
			name: "successful deletion",
			setupRepo: func(repo *mockRepository) {
				service := NewCircuitBreakerService(repo)
				service.CreateCircuitBreaker("test-id", DefaultConfig())
			},
			id: "test-id",
		},
		{
			name: "repository error",
			setupRepo: func(repo *mockRepository) {
				repo.deleteError = errors.New("delete error")
			},
			id:          "test-id",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			if tt.setupRepo != nil {
				tt.setupRepo(repo)
			}

			err := service.DeleteCircuitBreaker(tt.id)

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}

			if !tt.expectError {
				service.mu.RLock()
				_, exists := service.cache[tt.id]
				service.mu.RUnlock()

				if exists {
					t.Errorf("expected circuit breaker to be removed from cache")
				}
			}
		})
	}
}

func TestListCircuitBreakers(t *testing.T) {
	tests := []struct {
		name        string
		setupRepo   func(*mockRepository, *CircuitBreakerService)
		expectError bool
		expectedLen int
	}{
		{
			name: "list multiple circuit breakers",
			setupRepo: func(repo *mockRepository, service *CircuitBreakerService) {
				service.CreateCircuitBreaker("id1", DefaultConfig())
				service.CreateCircuitBreaker("id2", DefaultConfig())
			},
			expectedLen: 2,
		},
		{
			name: "repository error",
			setupRepo: func(repo *mockRepository, service *CircuitBreakerService) {
				repo.listError = errors.New("list error")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			if tt.setupRepo != nil {
				tt.setupRepo(repo, service)
			}

			ids, err := service.ListCircuitBreakers()

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if !tt.expectError && len(ids) != tt.expectedLen {
				t.Errorf("expected %d IDs, got %d", tt.expectedLen, len(ids))
			}
		})
	}
}

func TestHealthCheck(t *testing.T) {
	tests := []struct {
		name        string
		setupRepo   func(*mockRepository)
		expectError bool
	}{
		{
			name: "healthy",
		},
		{
			name: "repository error",
			setupRepo: func(repo *mockRepository) {
				repo.listError = errors.New("repository down")
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			service := NewCircuitBreakerService(repo)

			if tt.setupRepo != nil {
				tt.setupRepo(repo)
			}

			err := service.HealthCheck()

			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got %v", err)
			}
			if repo.listCalls != 1 {
				t.Errorf("expected 1 list call, got %d", repo.listCalls)
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	var wg sync.WaitGroup
	numGoroutines := 100
	numOperations := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cbID := fmt.Sprintf("cb-%d", id%5)

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

func TestUtilityMethods(t *testing.T) {
	repo := newMockRepository()
	service := NewCircuitBreakerService(repo)

	t.Run("get and update config", func(t *testing.T) {
		newConfig := Config{
			FailureThreshold: 10,
			SuccessThreshold: 5,
			Timeout:          3 * time.Second,
			CooldownPeriod:   15 * time.Second,
		}

		service.UpdateConfig(newConfig)
		currentConfig := service.GetConfig()

		if currentConfig != newConfig {
			t.Errorf("expected config %+v, got %+v", newConfig, currentConfig)
		}
	})

	t.Run("clear cache", func(t *testing.T) {
		service.Execute("id1", func() error { return nil })
		service.Execute("id2", func() error { return nil })

		service.ClearCache()

		service.mu.RLock()
		cacheSize := len(service.cache)
		service.mu.RUnlock()

		if cacheSize != 0 {
			t.Errorf("expected empty cache, got %d entries", cacheSize)
		}
	})

	t.Run("get stats", func(t *testing.T) {
		service.Execute("stats-test", func() error { return nil })
		service.Execute("stats-test", func() error { return errors.New("test error") })

		stats, err := service.GetStats("stats-test")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if stats.State != core.StateClosed {
			t.Errorf("expected state CLOSED, got %v", stats.State)
		}
		if stats.SuccessCount != 1 {
			t.Errorf("expected success count 1, got %d", stats.SuccessCount)
		}
	})
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
