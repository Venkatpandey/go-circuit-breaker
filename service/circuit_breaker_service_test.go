package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go-circuit-breaker/core"
)

type mockStore struct {
	mu          sync.Mutex
	data        map[string]core.Snapshot
	loadCalls   int
	saveCalls   int
	deleteCalls int
	listCalls   int
	loadErr     error
	saveErr     error
	deleteErr   error
	listErr     error
}

func newMockStore() *mockStore {
	return &mockStore{data: make(map[string]core.Snapshot)}
}

func (m *mockStore) Load(_ context.Context, id string) (*core.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loadCalls++
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	snapshot, ok := m.data[id]
	if !ok {
		return nil, nil
	}
	copy := snapshot
	return &copy, nil
}

func (m *mockStore) Save(_ context.Context, id string, snapshot core.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveCalls++
	if m.saveErr != nil {
		return m.saveErr
	}
	m.data[id] = snapshot
	return nil
}

func (m *mockStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleteCalls++
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, id)
	return nil
}

func (m *mockStore) List(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listCalls++
	if m.listErr != nil {
		return nil, m.listErr
	}
	ids := make([]string, 0, len(m.data))
	for id := range m.data {
		ids = append(ids, id)
	}
	return ids, nil
}

func managerBreakerConfig() core.Config {
	return core.Config{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		CooldownPeriod:   20 * time.Millisecond,
	}
}

func TestNewManager(t *testing.T) {
	manager, err := NewManager(DefaultOptions())
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	if manager == nil {
		t.Fatal("expected manager")
	}
}

func TestExecuteCreatesAndCachesBreaker(t *testing.T) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Store:         store,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	err = manager.Execute(context.Background(), "payments", func(context.Context) error { return nil })
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	if store.saveCalls != 1 {
		t.Fatalf("expected initial snapshot save, got %d", store.saveCalls)
	}

	_, _, err = manager.getOrCreate(context.Background(), "payments")
	if err != nil {
		t.Fatalf("get or create: %v", err)
	}
	if store.loadCalls != 1 {
		t.Fatalf("expected a single load call, got %d", store.loadCalls)
	}
}

func TestPersistOnStateTransitionOnly(t *testing.T) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Store:         store,
		PersistPolicy: PersistOnStateChange,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_ = manager.Execute(context.Background(), "db", func(context.Context) error { return nil })
	firstSaveCount := store.saveCalls

	_ = manager.Execute(context.Background(), "db", func(context.Context) error { return errors.New("boom") })
	if store.saveCalls != firstSaveCount {
		t.Fatalf("expected no save without state transition, got %d saves", store.saveCalls)
	}

	_ = manager.Execute(context.Background(), "db", func(context.Context) error { return errors.New("boom") })
	if store.saveCalls != firstSaveCount+1 {
		t.Fatalf("expected save on open transition, got %d saves", store.saveCalls)
	}
}

func TestPersistAlways(t *testing.T) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Store:         store,
		PersistPolicy: PersistAlways,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.Execute(context.Background(), "search", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if err := manager.Execute(context.Background(), "search", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if store.saveCalls < 2 {
		t.Fatalf("expected save on each execution, got %d", store.saveCalls)
	}
}

func TestForceOpenAndClose(t *testing.T) {
	manager, err := NewManager(Options{BreakerConfig: managerBreakerConfig()})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.ForceOpen(context.Background(), "api"); err != nil {
		t.Fatalf("force open: %v", err)
	}

	state, err := manager.State(context.Background(), "api")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != core.StateOpen {
		t.Fatalf("expected open, got %s", state)
	}

	if err := manager.ForceClose(context.Background(), "api"); err != nil {
		t.Fatalf("force close: %v", err)
	}

	state, err = manager.State(context.Background(), "api")
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	if state != core.StateClosed {
		t.Fatalf("expected closed, got %s", state)
	}
}

func TestCreateWithCustomConfig(t *testing.T) {
	manager, err := NewManager(Options{BreakerConfig: managerBreakerConfig()})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	custom := core.Config{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		CooldownPeriod:   time.Second,
	}
	cb, err := manager.Create(context.Background(), "worker", custom)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if cb.Config() != custom {
		t.Fatalf("expected custom config %+v, got %+v", custom, cb.Config())
	}
}

func TestDeleteAndList(t *testing.T) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Store:         store,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if _, err := manager.Create(context.Background(), "a", managerBreakerConfig()); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := manager.Create(context.Background(), "b", managerBreakerConfig()); err != nil {
		t.Fatalf("create b: %v", err)
	}

	ids, err := manager.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %d", len(ids))
	}

	if err := manager.Delete(context.Background(), "a"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if store.deleteCalls != 1 {
		t.Fatalf("expected delete to hit store once, got %d", store.deleteCalls)
	}
}

func TestHealthCheck(t *testing.T) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Store:         store,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.HealthCheck(context.Background()); err != nil {
		t.Fatalf("health check: %v", err)
	}
	if store.listCalls != 1 {
		t.Fatalf("expected list to be called once, got %d", store.listCalls)
	}
}

func BenchmarkManagerExecuteHotPath(b *testing.B) {
	manager, err := NewManager(Options{
		BreakerConfig: core.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			CooldownPeriod:   time.Minute,
		},
		PersistPolicy: PersistNever,
	})
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}

	ctx := context.Background()
	if err := manager.Execute(ctx, "payments", func(context.Context) error { return nil }); err != nil {
		b.Fatalf("warm manager: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := manager.Execute(ctx, "payments", func(context.Context) error { return nil }); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

func BenchmarkManagerExecutePersistAlways(b *testing.B) {
	store := newMockStore()
	manager, err := NewManager(Options{
		BreakerConfig: core.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			CooldownPeriod:   time.Minute,
		},
		Store:         store,
		PersistPolicy: PersistAlways,
	})
	if err != nil {
		b.Fatalf("new manager: %v", err)
	}

	ctx := context.Background()
	if err := manager.Execute(ctx, "payments", func(context.Context) error { return nil }); err != nil {
		b.Fatalf("warm manager: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := manager.Execute(ctx, "payments", func(context.Context) error { return nil }); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}
