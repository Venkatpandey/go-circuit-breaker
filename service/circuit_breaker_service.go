package service

import (
	"context"
	"fmt"
	"go-circuit-breaker/core"
	"go-circuit-breaker/ports"
	"sync"
	"time"
)

// Config holds the default configuration for new circuit breakers
type Config struct {
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	Timeout          time.Duration `json:"timeout"`
	CooldownPeriod   time.Duration `json:"cooldown_period"`
}

// DefaultConfig returns a sensible default configuration
func DefaultConfig() Config {
	return Config{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		Timeout:          1 * time.Second,
		CooldownPeriod:   5 * time.Second,
	}
}

type CircuitBreakerService struct {
	repo   ports.CircuitBreakerRepository
	config Config
	mu     sync.RWMutex
	cache  map[string]*core.CircuitBreaker
}

func NewCircuitBreakerService(repo ports.CircuitBreakerRepository) *CircuitBreakerService {
	return NewCircuitBreakerServiceWithConfig(repo, DefaultConfig())
}

func NewCircuitBreakerServiceWithConfig(repo ports.CircuitBreakerRepository, config Config) *CircuitBreakerService {
	return &CircuitBreakerService{
		repo:   repo,
		config: config,
		cache:  make(map[string]*core.CircuitBreaker),
	}
}

// Execute runs a function through the circuit breaker identified by the given ID
func (s *CircuitBreakerService) Execute(id string, fn func() error) error {
	return s.ExecuteWithContext(context.Background(), id, fn)
}

// ExecuteWithContext runs a function through the circuit breaker with context support
func (s *CircuitBreakerService) ExecuteWithContext(ctx context.Context, id string, fn func() error) error {
	cb, err := s.getOrCreateCircuitBreaker(id)
	if err != nil {
		return fmt.Errorf("failed to get circuit breaker for id %s: %w", id, err)
	}

	// Execute the function
	execErr := cb.ExecuteWithContext(ctx, fn)

	// Save the updated state (regardless of execution result)
	if saveErr := s.saveCircuitBreaker(id, cb); saveErr != nil {
		// Log the save error but don't override the execution error
		// In a real application, you'd use a proper logger here
		fmt.Printf("Warning: failed to save circuit breaker state for id %s: %v\n", id, saveErr)
	}

	return execErr
}

// GetStats returns the current statistics for a circuit breaker
func (s *CircuitBreakerService) GetStats(id string) (core.Stats, error) {
	cb, err := s.getOrCreateCircuitBreaker(id)
	if err != nil {
		return core.Stats{}, fmt.Errorf("failed to get circuit breaker for id %s: %w", id, err)
	}
	return cb.GetStats(), nil
}

// GetState returns the current state for a circuit breaker
func (s *CircuitBreakerService) GetState(id string) (core.State, error) {
	cb, err := s.getOrCreateCircuitBreaker(id)
	if err != nil {
		return core.StateClosed, fmt.Errorf("failed to get circuit breaker for id %s: %w", id, err)
	}
	return cb.GetState(), nil
}

// ForceOpen manually opens a circuit breaker
func (s *CircuitBreakerService) ForceOpen(id string) error {
	cb, err := s.getOrCreateCircuitBreaker(id)
	if err != nil {
		return fmt.Errorf("failed to get circuit breaker for id %s: %w", id, err)
	}

	cb.ForceOpen()

	if err := s.saveCircuitBreaker(id, cb); err != nil {
		return fmt.Errorf("failed to save circuit breaker state for id %s: %w", id, err)
	}

	return nil
}

// ForceClose manually closes a circuit breaker
func (s *CircuitBreakerService) ForceClose(id string) error {
	cb, err := s.getOrCreateCircuitBreaker(id)
	if err != nil {
		return fmt.Errorf("failed to get circuit breaker for id %s: %w", id, err)
	}

	cb.ForceClose()

	if err := s.saveCircuitBreaker(id, cb); err != nil {
		return fmt.Errorf("failed to save circuit breaker state for id %s: %w", id, err)
	}

	return nil
}

// CreateCircuitBreaker creates a new circuit breaker with custom configuration
func (s *CircuitBreakerService) CreateCircuitBreaker(id string, config Config) error {
	if id == "" {
		return fmt.Errorf("circuit breaker ID cannot be empty")
	}

	cb, err := core.NewCircuitBreaker(
		config.FailureThreshold,
		config.SuccessThreshold,
		config.Timeout,
		config.CooldownPeriod,
	)
	if err != nil {
		return fmt.Errorf("failed to create circuit breaker: %w", err)
	}

	// Save to repository
	if err := s.saveCircuitBreaker(id, cb); err != nil {
		return fmt.Errorf("failed to save circuit breaker: %w", err)
	}

	// Cache it
	s.mu.Lock()
	s.cache[id] = cb
	s.mu.Unlock()

	return nil
}

// DeleteCircuitBreaker removes a circuit breaker
func (s *CircuitBreakerService) DeleteCircuitBreaker(id string) error {
	// Remove from repository
	if err := s.repo.Delete(id); err != nil {
		return fmt.Errorf("failed to delete circuit breaker from repository: %w", err)
	}

	// Remove from cache
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()

	return nil
}

// ListCircuitBreakers returns all circuit breaker IDs
func (s *CircuitBreakerService) ListCircuitBreakers() ([]string, error) {
	return s.repo.List()
}

// getOrCreateCircuitBreaker retrieves an existing circuit breaker or creates a new one
func (s *CircuitBreakerService) getOrCreateCircuitBreaker(id string) (*core.CircuitBreaker, error) {
	if id == "" {
		return nil, fmt.Errorf("circuit breaker ID cannot be empty")
	}

	// Try to get from cache first
	s.mu.RLock()
	if cb, exists := s.cache[id]; exists {
		s.mu.RUnlock()
		return cb, nil
	}
	s.mu.RUnlock()

	// Try to load from repository
	cb, err := s.repo.FindByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to load circuit breaker from repository: %w", err)
	}

	// If not found, create a new one with default config
	if cb == nil {
		cb, err = core.NewCircuitBreaker(
			s.config.FailureThreshold,
			s.config.SuccessThreshold,
			s.config.Timeout,
			s.config.CooldownPeriod,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create new circuit breaker: %w", err)
		}

		// Save the new circuit breaker
		if err := s.saveCircuitBreaker(id, cb); err != nil {
			return nil, fmt.Errorf("failed to save new circuit breaker: %w", err)
		}
	}

	// Cache it
	s.mu.Lock()
	s.cache[id] = cb
	s.mu.Unlock()

	return cb, nil
}

// saveCircuitBreaker saves a circuit breaker to the repository
func (s *CircuitBreakerService) saveCircuitBreaker(id string, cb *core.CircuitBreaker) error {
	return s.repo.Save(id, cb)
}

// ClearCache removes all cached circuit breakers (useful for testing)
func (s *CircuitBreakerService) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache = make(map[string]*core.CircuitBreaker)
}

// UpdateConfig updates the default configuration for new circuit breakers
func (s *CircuitBreakerService) UpdateConfig(config Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// GetConfig returns the current default configuration
func (s *CircuitBreakerService) GetConfig() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

// HealthCheck verifies the service and repository are working
func (s *CircuitBreakerService) HealthCheck() error {
	// Try to list circuit breakers to verify repository connectivity
	_, err := s.repo.List()
	if err != nil {
		return fmt.Errorf("repository health check failed: %w", err)
	}
	return nil
}
