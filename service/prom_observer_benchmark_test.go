package service_test

import (
	"context"
	"testing"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
	promobs "github.com/Venkatpandey/go-circuit-breaker/observability/prometheus"
	"github.com/Venkatpandey/go-circuit-breaker/service"
)

func BenchmarkManagerExecuteWithPromObserver(b *testing.B) {
	observer := promobs.NewObserver(promobs.DefaultConfig())
	manager, err := service.NewManager(service.Options{
		BreakerConfig: core.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			CooldownPeriod:   time.Minute,
		},
		PersistPolicy: service.PersistNever,
		Observer:      observer,
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
