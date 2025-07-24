package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen   = errors.New("circuit breaker is open")
	ErrMaxAttempts   = errors.New("max attempts reached")
	ErrInvalidState  = errors.New("invalid state")
	ErrTimeout       = errors.New("operation timeout")
	ErrInvalidConfig = errors.New("invalid configuration")
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

func (s State) String() string {
	switch s {
	case StateClosed:
		return "CLOSED"
	case StateOpen:
		return "OPEN"
	case StateHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

type CircuitBreaker struct {
	mu               sync.RWMutex
	state            State         `json:"state"`
	failureThreshold int           `json:"failure_threshold"`
	successThreshold int           `json:"success_threshold"`
	failureCount     int           `json:"failure_count"`
	successCount     int           `json:"success_count"`
	timeout          time.Duration `json:"timeout"`
	lastFailureTime  time.Time     `json:"last_failure_time"`
	cooldownPeriod   time.Duration `json:"cooldown_period"`
}

type Stats struct {
	State        State     `json:"state"`
	FailureCount int       `json:"failure_count"`
	SuccessCount int       `json:"success_count"`
	LastFailure  time.Time `json:"last_failure_time"`
}

func NewCircuitBreaker(failureThreshold, successThreshold int, timeout, cooldownPeriod time.Duration) (*CircuitBreaker, error) {
	if failureThreshold <= 0 {
		return nil, errors.New("failure threshold must be positive")
	}
	if successThreshold <= 0 {
		return nil, errors.New("success threshold must be positive")
	}
	if timeout <= 0 {
		return nil, errors.New("timeout must be positive")
	}
	if cooldownPeriod <= 0 {
		return nil, errors.New("cooldown period must be positive")
	}

	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: failureThreshold,
		successThreshold: successThreshold,
		timeout:          timeout,
		cooldownPeriod:   cooldownPeriod,
	}, nil
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	return cb.ExecuteWithContext(context.Background(), fn)
}

func (cb *CircuitBreaker) ExecuteWithContext(ctx context.Context, fn func() error) error {
	// Check if we can execute
	if !cb.allowRequest() {
		return ErrCircuitOpen
	}

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, cb.timeout)
	defer cancel()

	// Execute with timeout
	resultChan := make(chan error, 1)
	go func() {
		resultChan <- fn()
	}()

	select {
	case err := <-resultChan:
		cb.recordResult(err)
		return err
	case <-timeoutCtx.Done():
		cb.recordResult(ErrTimeout)
		return ErrTimeout
	}
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case StateClosed:
		return true
	case StateOpen:
		if time.Since(cb.lastFailureTime) >= cb.cooldownPeriod {
			cb.state = StateHalfOpen
			cb.resetCounts()
			return true
		}
		return false
	case StateHalfOpen:
		return true
	default:
		return false
	}
}

func (cb *CircuitBreaker) recordResult(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.onFailure()
	} else {
		cb.onSuccess()
	}
}

func (cb *CircuitBreaker) onFailure() {
	cb.failureCount++

	switch cb.state {
	case StateClosed:
		if cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
			cb.lastFailureTime = time.Now()
		}
	case StateHalfOpen:
		cb.state = StateOpen
		cb.lastFailureTime = time.Now()
		cb.resetCounts()
	}
}

func (cb *CircuitBreaker) onSuccess() {
	cb.successCount++

	switch cb.state {
	case StateClosed:
		// Reset failure count on success in closed state
		cb.failureCount = 0
	case StateHalfOpen:
		if cb.successCount >= cb.successThreshold {
			cb.state = StateClosed
			cb.resetCounts()
		}
	}
}

func (cb *CircuitBreaker) resetCounts() {
	cb.failureCount = 0
	cb.successCount = 0
}

// GetStats returns current circuit breaker statistics
func (cb *CircuitBreaker) GetStats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		State:        cb.state,
		FailureCount: cb.failureCount,
		SuccessCount: cb.successCount,
		LastFailure:  cb.lastFailureTime,
	}
}

// GetState returns current state (thread-safe)
func (cb *CircuitBreaker) GetState() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// ForceOpen manually opens the circuit breaker
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateOpen
	cb.lastFailureTime = time.Now()
}

// ForceClose manually closes the circuit breaker
func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = StateClosed
	cb.resetCounts()
}
