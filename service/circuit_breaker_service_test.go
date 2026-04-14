package service

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
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

type recordingObserver struct {
	mu     sync.Mutex
	events []Event
}

func (o *recordingObserver) OnCircuitBreakerEvent(_ context.Context, event Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, event)
}

func (o *recordingObserver) snapshot() []Event {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]Event, len(o.events))
	copy(out, o.events)
	return out
}

func TestEventOrderAndPayloadForOpenTransition(t *testing.T) {
	observer := &recordingObserver{}
	manager, err := NewManager(Options{
		BreakerConfig: core.Config{
			FailureThreshold: 1,
			SuccessThreshold: 1,
			CooldownPeriod:   time.Second,
		},
		Observer: observer,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	execErr := manager.Execute(context.Background(), "payments", func(context.Context) error {
		return errors.New("boom")
	})
	if execErr == nil {
		t.Fatal("expected execution failure")
	}

	events := observer.snapshot()
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[0].Type != EventAllowGranted {
		t.Fatalf("expected first event allow granted, got %q", events[0].Type)
	}
	if events[1].Type != EventExecutionFinished || events[1].Outcome != OutcomeFailure {
		t.Fatalf("expected execution failure event, got type=%q outcome=%q", events[1].Type, events[1].Outcome)
	}
	if events[2].Type != EventStateTransition || events[2].Before != core.StateClosed || events[2].After != core.StateOpen {
		t.Fatalf("unexpected transition event: %+v", events[2])
	}
}

func TestAllowDeniedEmitsBlockedOpen(t *testing.T) {
	observer := &recordingObserver{}
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Observer:      observer,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.ForceOpen(context.Background(), "api"); err != nil {
		t.Fatalf("force open: %v", err)
	}
	observer.events = nil

	err = manager.Execute(context.Background(), "api", func(context.Context) error { return nil })
	if !errors.Is(err, core.ErrCircuitOpen) {
		t.Fatalf("expected circuit open error, got %v", err)
	}

	events := observer.snapshot()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventAllowDenied || events[0].Outcome != OutcomeBlockedOpen {
		t.Fatalf("unexpected allow denied event: %+v", events[0])
	}
	if events[1].Type != EventExecutionFinished || events[1].Outcome != OutcomeBlockedOpen {
		t.Fatalf("unexpected execution event: %+v", events[1])
	}
}

func TestProbeEventEmitted(t *testing.T) {
	observer := &recordingObserver{}
	manager, err := NewManager(Options{
		BreakerConfig: core.Config{
			FailureThreshold: 1,
			SuccessThreshold: 2,
			CooldownPeriod:   20 * time.Millisecond,
		},
		Observer: observer,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.ForceOpen(context.Background(), "svc"); err != nil {
		t.Fatalf("force open: %v", err)
	}
	observer.events = nil
	time.Sleep(30 * time.Millisecond)

	if err := manager.Execute(context.Background(), "svc", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("execute probe: %v", err)
	}

	events := observer.snapshot()
	hasProbe := false
	for _, event := range events {
		if event.Type == EventProbeStarted {
			hasProbe = true
			break
		}
	}
	if !hasProbe {
		t.Fatalf("expected probe event, got %+v", events)
	}
}

func TestObserverPanicIsIsolated(t *testing.T) {
	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Observer: ObserverFunc(func(context.Context, Event) {
			panic("observer panic")
		}),
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	if err := manager.Execute(context.Background(), "svc", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("observer panic should not affect execute, got %v", err)
	}
}

func TestObserverPanicCallsErrorHandler(t *testing.T) {
	var (
		mu     sync.Mutex
		called bool
	)

	manager, err := NewManager(Options{
		BreakerConfig: managerBreakerConfig(),
		Observer: ObserverFunc(func(context.Context, Event) {
			panic("observer panic")
		}),
		ObserverErrHandler: func(err error) {
			mu.Lock()
			defer mu.Unlock()
			called = true
		},
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}

	_ = manager.Execute(context.Background(), "svc", func(context.Context) error { return nil })
	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("expected observer error handler to be called")
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

func BenchmarkManagerExecuteWithNoopObserver(b *testing.B) {
	manager, err := NewManager(Options{
		BreakerConfig: core.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			CooldownPeriod:   time.Minute,
		},
		PersistPolicy: PersistNever,
		Observer:      ObserverFunc(func(context.Context, Event) {}),
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

func TestComposeObservers(t *testing.T) {
	var out []string
	o1 := ObserverFunc(func(_ context.Context, _ Event) { out = append(out, "one") })
	o2 := ObserverFunc(func(_ context.Context, _ Event) { out = append(out, "two") })

	composed := ComposeObservers(nil, o1, o2)
	if composed == nil {
		t.Fatal("expected composed observer")
	}

	composed.OnCircuitBreakerEvent(context.Background(), Event{})
	if !slices.Equal(out, []string{"one", "two"}) {
		t.Fatalf("unexpected observer call order: %v", out)
	}
}
