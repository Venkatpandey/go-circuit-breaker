package core

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrCircuitOpen   = errors.New("circuit breaker is open")
	ErrInvalidState  = errors.New("invalid state")
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

type Config struct {
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	CooldownPeriod   time.Duration `json:"cooldown_period"`
	RequestTimeout   time.Duration `json:"request_timeout"`
}

func DefaultConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownPeriod:   30 * time.Second,
		RequestTimeout:   0,
	}
}

func (c Config) Validate() error {
	switch {
	case c.FailureThreshold <= 0:
		return errors.New("failure threshold must be positive")
	case c.SuccessThreshold <= 0:
		return errors.New("success threshold must be positive")
	case c.CooldownPeriod <= 0:
		return errors.New("cooldown period must be positive")
	case c.RequestTimeout < 0:
		return errors.New("request timeout cannot be negative")
	default:
		return nil
	}
}

type Stats struct {
	State                 State     `json:"state"`
	ConsecutiveFailures   int       `json:"consecutive_failures"`
	ConsecutiveSuccesses  int       `json:"consecutive_successes"`
	LastFailureTime       time.Time `json:"last_failure_time"`
	LastStateChangeTime   time.Time `json:"last_state_change_time"`
	HalfOpenProbeInFlight bool      `json:"half_open_probe_in_flight"`
}

type Snapshot struct {
	Config Config `json:"config"`
	Stats  Stats  `json:"stats"`
}

type Completion func(err error)

type CircuitBreaker struct {
	mu                    sync.RWMutex
	config                Config
	state                 State
	consecutiveFailures   int
	consecutiveSuccesses  int
	lastFailureTime       time.Time
	lastStateChangeTime   time.Time
	halfOpenProbeInFlight bool
}

func NewCircuitBreaker(config Config) (*CircuitBreaker, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	now := time.Now()
	return &CircuitBreaker{
		config:              config,
		state:               StateClosed,
		lastStateChangeTime: now,
	}, nil
}

func NewCircuitBreakerFromSnapshot(snapshot Snapshot) (*CircuitBreaker, error) {
	if err := snapshot.Config.Validate(); err != nil {
		return nil, err
	}
	if snapshot.Stats.State < StateClosed || snapshot.Stats.State > StateHalfOpen {
		return nil, ErrInvalidState
	}

	cb := &CircuitBreaker{
		config:                snapshot.Config,
		state:                 snapshot.Stats.State,
		consecutiveFailures:   snapshot.Stats.ConsecutiveFailures,
		consecutiveSuccesses:  snapshot.Stats.ConsecutiveSuccesses,
		lastFailureTime:       snapshot.Stats.LastFailureTime,
		lastStateChangeTime:   snapshot.Stats.LastStateChangeTime,
		halfOpenProbeInFlight: snapshot.Stats.HalfOpenProbeInFlight,
	}

	if cb.lastStateChangeTime.IsZero() {
		cb.lastStateChangeTime = time.Now()
	}

	return cb, nil
}

func (cb *CircuitBreaker) Execute(ctx context.Context, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	done, err := cb.Allow()
	if err != nil {
		return err
	}

	execCtx := ctx
	cancel := func() {}
	if cb.config.RequestTimeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, cb.config.RequestTimeout)
	}
	defer cancel()

	err = fn(execCtx)
	done(err)
	return err
}

func (cb *CircuitBreaker) Allow() (Completion, error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return cb.newCompletion(false), nil
	case StateOpen:
		if now.Sub(cb.lastFailureTime) < cb.config.CooldownPeriod {
			return nil, ErrCircuitOpen
		}
		cb.transitionToLocked(StateHalfOpen, now)
		cb.consecutiveFailures = 0
		cb.consecutiveSuccesses = 0
		cb.halfOpenProbeInFlight = false
	case StateHalfOpen:
	default:
		return nil, ErrInvalidState
	}

	if cb.halfOpenProbeInFlight {
		return nil, ErrCircuitOpen
	}

	cb.halfOpenProbeInFlight = true
	return cb.newCompletion(true), nil
}

func (cb *CircuitBreaker) State() State {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

func (cb *CircuitBreaker) Stats() Stats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Stats{
		State:                 cb.state,
		ConsecutiveFailures:   cb.consecutiveFailures,
		ConsecutiveSuccesses:  cb.consecutiveSuccesses,
		LastFailureTime:       cb.lastFailureTime,
		LastStateChangeTime:   cb.lastStateChangeTime,
		HalfOpenProbeInFlight: cb.halfOpenProbeInFlight,
	}
}

func (cb *CircuitBreaker) Snapshot() Snapshot {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return Snapshot{
		Config: cb.config,
		Stats: Stats{
			State:                 cb.state,
			ConsecutiveFailures:   cb.consecutiveFailures,
			ConsecutiveSuccesses:  cb.consecutiveSuccesses,
			LastFailureTime:       cb.lastFailureTime,
			LastStateChangeTime:   cb.lastStateChangeTime,
			HalfOpenProbeInFlight: cb.halfOpenProbeInFlight,
		},
	}
}

func (cb *CircuitBreaker) Config() Config {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.config
}

func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	cb.transitionToLocked(StateOpen, now)
	cb.consecutiveFailures = cb.config.FailureThreshold
	cb.consecutiveSuccesses = 0
	cb.lastFailureTime = now
	cb.halfOpenProbeInFlight = false
}

func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.transitionToLocked(StateClosed, time.Now())
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.halfOpenProbeInFlight = false
}

func (cb *CircuitBreaker) newCompletion(halfOpenProbe bool) Completion {
	var once sync.Once
	return func(err error) {
		once.Do(func() {
			cb.afterExecution(halfOpenProbe, err)
		})
	}
}

func (cb *CircuitBreaker) afterExecution(halfOpenProbe bool, err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	if halfOpenProbe {
		cb.halfOpenProbeInFlight = false
	}

	if err == nil {
		cb.onSuccessLocked(now)
		return
	}

	cb.onFailureLocked(now)
}

func (cb *CircuitBreaker) onSuccessLocked(now time.Time) {
	switch cb.state {
	case StateClosed:
		cb.consecutiveFailures = 0
		cb.consecutiveSuccesses++
	case StateHalfOpen:
		cb.consecutiveFailures = 0
		cb.consecutiveSuccesses++
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.transitionToLocked(StateClosed, now)
			cb.consecutiveFailures = 0
			cb.consecutiveSuccesses = 0
		}
	}
}

func (cb *CircuitBreaker) onFailureLocked(now time.Time) {
	cb.lastFailureTime = now

	switch cb.state {
	case StateClosed:
		cb.consecutiveFailures++
		cb.consecutiveSuccesses = 0
		if cb.consecutiveFailures >= cb.config.FailureThreshold {
			cb.transitionToLocked(StateOpen, now)
		}
	case StateHalfOpen:
		cb.consecutiveFailures = 1
		cb.consecutiveSuccesses = 0
		cb.transitionToLocked(StateOpen, now)
	}
}

func (cb *CircuitBreaker) transitionToLocked(next State, now time.Time) {
	if cb.state == next {
		return
	}
	cb.state = next
	cb.lastStateChangeTime = now
}
