package service

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"go-circuit-breaker/core"
	"go-circuit-breaker/ports"
)

type PersistPolicy int

const (
	PersistOnStateChange PersistPolicy = iota
	PersistAlways
	PersistNever
)

type Options struct {
	BreakerConfig core.Config
	Store         ports.SnapshotStore
	PersistPolicy PersistPolicy
}

func DefaultOptions() Options {
	return Options{
		BreakerConfig: core.DefaultConfig(),
		PersistPolicy: PersistOnStateChange,
	}
}

type Manager struct {
	store         ports.SnapshotStore
	breakerConfig core.Config
	persistPolicy PersistPolicy

	mu       sync.RWMutex
	breakers map[string]*core.CircuitBreaker
}

func NewManager(options Options) (*Manager, error) {
	if options.BreakerConfig == (core.Config{}) {
		options.BreakerConfig = core.DefaultConfig()
	}
	if err := options.BreakerConfig.Validate(); err != nil {
		return nil, err
	}

	return &Manager{
		store:         options.Store,
		breakerConfig: options.BreakerConfig,
		persistPolicy: options.PersistPolicy,
		breakers:      make(map[string]*core.CircuitBreaker),
	}, nil
}

func (m *Manager) Execute(ctx context.Context, id string, fn func(context.Context) error) error {
	cb, created, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	before := cb.State()
	err = cb.Execute(ctx, fn)
	after := cb.State()

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

func (m *Manager) Get(ctx context.Context, id string) (*core.CircuitBreaker, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	return cb, err
}

func (m *Manager) Stats(ctx context.Context, id string) (core.Stats, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return core.Stats{}, err
	}
	return cb.Stats(), nil
}

func (m *Manager) State(ctx context.Context, id string) (core.State, error) {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return core.StateClosed, err
	}
	return cb.State(), nil
}

func (m *Manager) ForceOpen(ctx context.Context, id string) error {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	cb.ForceOpen()
	if shouldSave(m.persistPolicy, false, core.StateClosed, core.StateOpen) {
		return m.save(ctx, id, cb)
	}

	return nil
}

func (m *Manager) ForceClose(ctx context.Context, id string) error {
	cb, _, err := m.getOrCreate(ctx, id)
	if err != nil {
		return err
	}

	cb.ForceClose()
	if shouldSave(m.persistPolicy, false, core.StateOpen, core.StateClosed) {
		return m.save(ctx, id, cb)
	}

	return nil
}

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

func (m *Manager) ClearCache() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.breakers = make(map[string]*core.CircuitBreaker)
}

func (m *Manager) HealthCheck(ctx context.Context) error {
	if m.store == nil {
		return nil
	}

	_, err := m.store.List(ctx)
	return err
}

func (m *Manager) save(ctx context.Context, id string, cb *core.CircuitBreaker) error {
	if m.store == nil {
		return nil
	}
	if err := m.store.Save(ctx, id, cb.Snapshot()); err != nil {
		return fmt.Errorf("save snapshot for %q: %w", id, err)
	}
	return nil
}

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
