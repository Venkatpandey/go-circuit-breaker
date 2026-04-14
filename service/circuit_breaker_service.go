package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
	"github.com/Venkatpandey/go-circuit-breaker/ports"
)

// PersistPolicy controls when snapshots are written through the store.
type PersistPolicy int

const (
	// PersistOnStateChange writes snapshots only when state changes.
	PersistOnStateChange PersistPolicy = iota
	// PersistAlways writes snapshots after every execution.
	PersistAlways
	// PersistNever disables snapshot persistence.
	PersistNever
)

// Options configures a manager instance.
type Options struct {
	BreakerConfig      core.Config
	Store              ports.SnapshotStore
	PersistPolicy      PersistPolicy
	Observer           Observer
	Observers          []Observer
	ObserverErrHandler ObserverErrorHandler
}

// DefaultOptions returns a production-oriented default config.
func DefaultOptions() Options {
	return Options{
		BreakerConfig: core.DefaultConfig(),
		PersistPolicy: PersistOnStateChange,
	}
}

// Manager owns named in-process circuit breakers and optional persistence.
type Manager struct {
	store         ports.SnapshotStore
	breakerConfig core.Config
	persistPolicy PersistPolicy

	observers          []Observer
	observerErrHandler ObserverErrorHandler

	mu       sync.RWMutex
	breakers map[string]*core.CircuitBreaker
}

// --- Construction ---

// NewManager creates a manager with optional store and observers.
func NewManager(options Options) (*Manager, error) {
	if options.BreakerConfig == (core.Config{}) {
		options.BreakerConfig = core.DefaultConfig()
	}
	if err := options.BreakerConfig.Validate(); err != nil {
		return nil, err
	}

	observers := make([]Observer, 0, len(options.Observers)+1)
	if options.Observer != nil {
		observers = append(observers, options.Observer)
	}
	observers = append(observers, options.Observers...)
	filtered := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}

	return &Manager{
		store:              options.Store,
		breakerConfig:      options.BreakerConfig,
		persistPolicy:      options.PersistPolicy,
		observers:          filtered,
		observerErrHandler: options.ObserverErrHandler,
		breakers:           make(map[string]*core.CircuitBreaker),
	}, nil
}

// --- Execution and lifecycle API ---

// Execute runs fn through the named breaker and emits observability events.
func (m *Manager) Execute(ctx context.Context, id string, fn func(context.Context) error) error {
	if ctx == nil {
		ctx = context.Background()
	}

	cb, created, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	before := cb.State()

	if err := ctx.Err(); err != nil {
		m.emit(ctx, Event{
			Time:      time.Now(),
			BreakerID: id,
			Type:      EventExecutionFinished,
			Before:    before,
			After:     cb.State(),
			State:     cb.State(),
			Outcome:   classifyOutcome(err),
			Err:       err,
		})
		return err
	}

	done, allowErr := cb.Allow()
	if allowErr != nil {
		now := time.Now()
		state := cb.State()
		m.emit(ctx, Event{
			Time:      now,
			BreakerID: id,
			Type:      EventAllowDenied,
			Before:    before,
			After:     state,
			State:     state,
			Outcome:   classifyOutcome(allowErr),
			Err:       allowErr,
		})
		m.emit(ctx, Event{
			Time:      now,
			BreakerID: id,
			Type:      EventExecutionFinished,
			Before:    before,
			After:     state,
			State:     state,
			Outcome:   classifyOutcome(allowErr),
			Err:       allowErr,
		})
		return allowErr
	}

	allowState := cb.State()
	now := time.Now()
	m.emit(ctx, Event{
		Time:      now,
		BreakerID: id,
		Type:      EventAllowGranted,
		Before:    before,
		After:     allowState,
		State:     allowState,
		Outcome:   OutcomeUnknown,
	})

	if stats := cb.Stats(); stats.State == core.StateHalfOpen && stats.HalfOpenProbeInFlight {
		m.emit(ctx, Event{
			Time:      now,
			BreakerID: id,
			Type:      EventProbeStarted,
			Before:    before,
			After:     allowState,
			State:     allowState,
			Outcome:   OutcomeUnknown,
		})
	}

	execCtx := ctx
	cancel := func() {}
	if timeout := cb.Config().RequestTimeout; timeout > 0 {
		execCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	err = fn(execCtx)
	done(err)

	after := cb.State()
	eventTime := time.Now()
	outcome := classifyExecutionOutcome(ctx, execCtx, err)
	m.emit(ctx, Event{
		Time:      eventTime,
		BreakerID: id,
		Type:      EventExecutionFinished,
		Before:    before,
		After:     after,
		State:     after,
		Outcome:   outcome,
		Err:       err,
	})

	if before != after {
		m.emit(ctx, Event{
			Time:      eventTime,
			BreakerID: id,
			Type:      EventStateTransition,
			Before:    before,
			After:     after,
			State:     after,
			Outcome:   outcome,
			Err:       err,
		})
	}

	if created {
		return err
	}

	if shouldSave(m.persistPolicy, false, before, after) {
		if saveErr := m.save(ctx, id, cb); saveErr != nil {
			return errors.Join(err, saveErr)
		}
	}

	return err
}

// Create initializes and stores a breaker with custom configuration.
func (m *Manager) Create(ctx context.Context, id string, config core.Config) (*core.CircuitBreaker, error) {
	if id == "" {
		return nil, errors.New("circuit breaker id cannot be empty")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}

	cb, err := core.NewCircuitBreaker(config)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.breakers[id] = cb
	m.mu.Unlock()

	if m.store != nil && m.persistPolicy != PersistNever {
		if err := m.save(ctx, id, cb); err != nil {
			return nil, err
		}
	}

	return cb, nil
}

// Get returns a named breaker, creating it on first access.
func (m *Manager) Get(ctx context.Context, id string) (*core.CircuitBreaker, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	return cb, err
}

// Stats returns breaker statistics for id.
func (m *Manager) Stats(ctx context.Context, id string) (core.Stats, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return core.Stats{}, err
	}
	return cb.Stats(), nil
}

// State returns breaker state for id.
func (m *Manager) State(ctx context.Context, id string) (core.State, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return core.StateClosed, err
	}
	return cb.State(), nil
}

// ForceOpen forces breaker state to OPEN.
func (m *Manager) ForceOpen(ctx context.Context, id string) error {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	before := cb.State()
	cb.ForceOpen()
	after := cb.State()
	if before != after {
		m.emit(ctx, Event{
			Time:      time.Now(),
			BreakerID: id,
			Type:      EventStateTransition,
			Before:    before,
			After:     after,
			State:     after,
			Outcome:   OutcomeUnknown,
		})
	}

	if shouldSave(m.persistPolicy, false, core.StateClosed, core.StateOpen) {
		return m.save(ctx, id, cb)
	}

	return nil
}

// ForceClose forces breaker state to CLOSED.
func (m *Manager) ForceClose(ctx context.Context, id string) error {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	before := cb.State()
	cb.ForceClose()
	after := cb.State()
	if before != after {
		m.emit(ctx, Event{
			Time:      time.Now(),
			BreakerID: id,
			Type:      EventStateTransition,
			Before:    before,
			After:     after,
			State:     after,
			Outcome:   OutcomeUnknown,
		})
	}

	if shouldSave(m.persistPolicy, false, core.StateOpen, core.StateClosed) {
		return m.save(ctx, id, cb)
	}

	return nil
}

// Delete removes a breaker from memory and from the snapshot store.
func (m *Manager) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("circuit breaker id cannot be empty")
	}

	m.mu.Lock()
	delete(m.breakers, id)
	m.mu.Unlock()

	if m.store == nil {
		return nil
	}

	return m.store.Delete(ctx, id)
}

// List returns breaker IDs from store (if configured) or in-memory cache.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	if m.store != nil {
		return m.store.List(ctx)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := make([]string, 0, len(m.breakers))
	for id := range m.breakers {
		ids = append(ids, id)
	}
	return ids, nil
}

// ClearCache resets in-memory breaker cache.
func (m *Manager) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakers = make(map[string]*core.CircuitBreaker)
}

// HealthCheck verifies store connectivity.
func (m *Manager) HealthCheck(ctx context.Context) error {
	if m.store == nil {
		return nil
	}

	_, err := m.store.List(ctx)
	return err
}

// --- Internal helpers ---

// save persists breaker snapshot.
func (m *Manager) save(ctx context.Context, id string, cb *core.CircuitBreaker) error {
	if m.store == nil {
		return nil
	}
	if err := m.store.Save(ctx, id, cb.Snapshot()); err != nil {
		return fmt.Errorf("save snapshot for %q: %w", id, err)
	}
	return nil
}

// getOrCreate retrieves breaker from cache/store or creates a new one.
func (m *Manager) getOrCreate(ctx context.Context, id string) (*core.CircuitBreaker, bool, error) {
	if id == "" {
		return nil, false, errors.New("circuit breaker id cannot be empty")
	}

	m.mu.RLock()
	if cb, ok := m.breakers[id]; ok {
		m.mu.RUnlock()
		return cb, false, nil
	}
	m.mu.RUnlock()

	candidate, created, err := m.loadOrCreate(ctx, id)
	if err != nil {
		return nil, false, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if cb, ok := m.breakers[id]; ok {
		return cb, false, nil
	}
	m.breakers[id] = candidate
	return candidate, created, nil
}

// loadOrCreate loads snapshot and reconstructs breaker or creates a new one.
func (m *Manager) loadOrCreate(ctx context.Context, id string) (*core.CircuitBreaker, bool, error) {
	if m.store != nil {
		snapshot, err := m.store.Load(ctx, id)
		if err != nil {
			return nil, false, fmt.Errorf("load snapshot for %q: %w", id, err)
		}
		if snapshot != nil {
			cb, err := core.NewCircuitBreakerFromSnapshot(*snapshot)
			if err != nil {
				return nil, false, fmt.Errorf("restore snapshot for %q: %w", id, err)
			}
			return cb, false, nil
		}
	}

	cb, err := core.NewCircuitBreaker(m.breakerConfig)
	if err != nil {
		return nil, false, err
	}
	if m.store != nil && m.persistPolicy != PersistNever {
		if err := m.save(ctx, id, cb); err != nil {
			return nil, false, err
		}
	}
	return cb, true, nil
}

func shouldSave(policy PersistPolicy, created bool, before, after core.State) bool {
	switch policy {
	case PersistNever:
		return false
	case PersistAlways:
		return true
	default:
		return created || before != after
	}
}

func classifyOutcome(err error) Outcome {
	switch {
	case err == nil:
		return OutcomeSuccess
	case errors.Is(err, core.ErrCircuitOpen):
		return OutcomeBlockedOpen
	case errors.Is(err, context.Canceled):
		return OutcomeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return OutcomeDeadlineExceeded
	default:
		return OutcomeFailure
	}
}

func classifyExecutionOutcome(parentCtx, execCtx context.Context, err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	if errors.Is(err, core.ErrCircuitOpen) {
		return OutcomeBlockedOpen
	}
	if errors.Is(err, context.Canceled) {
		return OutcomeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		if parentCtx != nil && parentCtx.Err() == nil && execCtx != nil && errors.Is(execCtx.Err(), context.DeadlineExceeded) {
			return OutcomeTimeout
		}
		return OutcomeDeadlineExceeded
	}
	return OutcomeFailure
}

func (m *Manager) emit(ctx context.Context, event Event) {
	if len(m.observers) == 0 {
		return
	}

	for idx, observer := range m.observers {
		func(index int, o Observer) {
			// Observers are synchronous by design. We recover panics so hooks cannot break breaker flow.
			defer func() {
				if r := recover(); r != nil && m.observerErrHandler != nil {
					m.observerErrHandler(&ObserverError{
						ObserverIndex: index,
						EventType:     event.Type,
						Cause:         fmt.Errorf("panic: %v", r),
					})
				}
			}()
			o.OnCircuitBreakerEvent(ctx, event)
		}(idx, observer)
	}
}
