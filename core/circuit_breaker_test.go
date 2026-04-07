package core

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{
		FailureThreshold: 2,
		SuccessThreshold: 2,
		CooldownPeriod:   40 * time.Millisecond,
		RequestTimeout:   25 * time.Millisecond,
	}
}

func createTestBreaker(t *testing.T) *CircuitBreaker {
	t.Helper()

	cb, err := NewCircuitBreaker(testConfig())
	if err != nil {
		t.Fatalf("create breaker: %v", err)
	}

	return cb
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		config Config
		valid  bool
	}{
		{name: "valid", config: testConfig(), valid: true},
		{name: "missing failure threshold", config: Config{SuccessThreshold: 1, CooldownPeriod: time.Second}, valid: false},
		{name: "missing success threshold", config: Config{FailureThreshold: 1, CooldownPeriod: time.Second}, valid: false},
		{name: "missing cooldown", config: Config{FailureThreshold: 1, SuccessThreshold: 1}, valid: false},
		{name: "negative timeout", config: Config{FailureThreshold: 1, SuccessThreshold: 1, CooldownPeriod: time.Second, RequestTimeout: -time.Second}, valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.valid && err != nil {
				t.Fatalf("expected valid config, got %v", err)
			}
			if !tt.valid && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestClosedToOpenAfterFailures(t *testing.T) {
	cb := createTestBreaker(t)
	testErr := errors.New("dependency failed")

	for i := 0; i < cb.Config().FailureThreshold; i++ {
		err := cb.Execute(context.Background(), func(context.Context) error { return testErr })
		if !errors.Is(err, testErr) {
			t.Fatalf("expected dependency error, got %v", err)
		}
	}

	if got := cb.State(); got != StateOpen {
		t.Fatalf("expected open state, got %s", got)
	}
}

func TestHalfOpenAllowsSingleProbe(t *testing.T) {
	cb := createTestBreaker(t)

	cb.ForceOpen()
	time.Sleep(cb.Config().CooldownPeriod + 10*time.Millisecond)

	done, err := cb.Allow()
	if err != nil {
		t.Fatalf("expected probe to be allowed, got %v", err)
	}

	if _, err := cb.Allow(); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("expected second probe to be blocked, got %v", err)
	}

	done(nil)

	if got := cb.State(); got != StateHalfOpen {
		t.Fatalf("expected half-open after one successful probe, got %s", got)
	}
}

func TestHalfOpenClosesAfterSuccessThreshold(t *testing.T) {
	cb := createTestBreaker(t)

	cb.ForceOpen()
	time.Sleep(cb.Config().CooldownPeriod + 10*time.Millisecond)

	for i := 0; i < cb.Config().SuccessThreshold; i++ {
		err := cb.Execute(context.Background(), func(context.Context) error { return nil })
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	}

	if got := cb.State(); got != StateClosed {
		t.Fatalf("expected closed state, got %s", got)
	}
}

func TestHalfOpenFailureReopensImmediately(t *testing.T) {
	cb := createTestBreaker(t)
	testErr := errors.New("probe failed")

	cb.ForceOpen()
	time.Sleep(cb.Config().CooldownPeriod + 10*time.Millisecond)

	err := cb.Execute(context.Background(), func(context.Context) error { return testErr })
	if !errors.Is(err, testErr) {
		t.Fatalf("expected probe error, got %v", err)
	}

	if got := cb.State(); got != StateOpen {
		t.Fatalf("expected breaker to reopen, got %s", got)
	}
}

func TestForceOpenAndClose(t *testing.T) {
	cb := createTestBreaker(t)

	cb.ForceOpen()
	if got := cb.State(); got != StateOpen {
		t.Fatalf("expected open after force open, got %s", got)
	}

	cb.ForceClose()
	if got := cb.State(); got != StateClosed {
		t.Fatalf("expected closed after force close, got %s", got)
	}
}

func TestExecuteUsesContextTimeout(t *testing.T) {
	cb := createTestBreaker(t)

	start := time.Now()
	err := cb.Execute(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	if elapsed := time.Since(start); elapsed < cb.Config().RequestTimeout {
		t.Fatalf("expected timeout to be applied, elapsed=%v", elapsed)
	}

	if got := cb.Stats().ConsecutiveFailures; got != 1 {
		t.Fatalf("expected timeout to count as failure, got %d", got)
	}
}

func TestExecuteReturnsCallerContextErrorWithoutTouchingBreaker(t *testing.T) {
	cb := createTestBreaker(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := cb.Execute(ctx, func(context.Context) error {
		t.Fatal("function should not be called")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context, got %v", err)
	}

	stats := cb.Stats()
	if stats.ConsecutiveFailures != 0 || stats.ConsecutiveSuccesses != 0 {
		t.Fatalf("expected untouched breaker stats, got %+v", stats)
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	cb := createTestBreaker(t)
	testErr := errors.New("failure")

	_ = cb.Execute(context.Background(), func(context.Context) error { return testErr })
	snapshot := cb.Snapshot()

	restored, err := NewCircuitBreakerFromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("restore breaker: %v", err)
	}

	if restored.State() != cb.State() {
		t.Fatalf("expected state %s, got %s", cb.State(), restored.State())
	}
	if restored.Stats().ConsecutiveFailures != cb.Stats().ConsecutiveFailures {
		t.Fatalf("expected failures %d, got %d", cb.Stats().ConsecutiveFailures, restored.Stats().ConsecutiveFailures)
	}
}

func TestConcurrency(t *testing.T) {
	cb := createTestBreaker(t)

	var successes int64
	var failures int64
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := cb.Execute(context.Background(), func(context.Context) error {
				if i%11 == 0 {
					return errors.New("boom")
				}
				return nil
			})
			if err != nil {
				atomic.AddInt64(&failures, 1)
				return
			}
			atomic.AddInt64(&successes, 1)
		}(i)
	}

	wg.Wait()

	if successes == 0 {
		t.Fatal("expected some successful executions")
	}
	if failures == 0 {
		t.Fatal("expected some failed executions")
	}
}

func benchmarkConfig() Config {
	return Config{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		CooldownPeriod:   time.Minute,
		RequestTimeout:   0,
	}
}

func BenchmarkCircuitBreakerExecuteSuccess(b *testing.B) {
	cb, err := NewCircuitBreaker(benchmarkConfig())
	if err != nil {
		b.Fatalf("create breaker: %v", err)
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := cb.Execute(ctx, func(context.Context) error { return nil }); err != nil {
			b.Fatalf("execute: %v", err)
		}
	}
}

func BenchmarkCircuitBreakerAllowCompleteSuccess(b *testing.B) {
	cb, err := NewCircuitBreaker(benchmarkConfig())
	if err != nil {
		b.Fatalf("create breaker: %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		done, err := cb.Allow()
		if err != nil {
			b.Fatalf("allow: %v", err)
		}
		done(nil)
	}
}

func BenchmarkCircuitBreakerOpenFastFail(b *testing.B) {
	cb, err := NewCircuitBreaker(benchmarkConfig())
	if err != nil {
		b.Fatalf("create breaker: %v", err)
	}
	cb.ForceOpen()

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := cb.Execute(ctx, func(context.Context) error { return nil })
		if !errors.Is(err, ErrCircuitOpen) {
			b.Fatalf("expected ErrCircuitOpen, got %v", err)
		}
	}
}

func BenchmarkCircuitBreakerExecuteParallel(b *testing.B) {
	cb, err := NewCircuitBreaker(benchmarkConfig())
	if err != nil {
		b.Fatalf("create breaker: %v", err)
	}

	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := cb.Execute(ctx, func(context.Context) error { return nil }); err != nil {
				b.Fatalf("execute: %v", err)
			}
		}
	})
}
