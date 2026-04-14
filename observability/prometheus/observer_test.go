package prometheus

import (
	"context"
	"testing"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
	"github.com/Venkatpandey/go-circuit-breaker/service"

	prom "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestObserverCollectorsAndMetrics(t *testing.T) {
	observer := NewObserver(DefaultConfig())
	registry := prom.NewRegistry()
	for _, collector := range observer.Collectors() {
		if err := registry.Register(collector); err != nil {
			t.Fatalf("register collector: %v", err)
		}
	}

	observer.OnCircuitBreakerEvent(context.Background(), service.Event{
		Time:      time.Now(),
		BreakerID: "payments",
		Type:      service.EventAllowGranted,
		State:     core.StateClosed,
		Outcome:   service.OutcomeUnknown,
	})
	observer.OnCircuitBreakerEvent(context.Background(), service.Event{
		Time:      time.Now(),
		BreakerID: "payments",
		Type:      service.EventExecutionFinished,
		State:     core.StateOpen,
		Outcome:   service.OutcomeBlockedOpen,
	})
	observer.OnCircuitBreakerEvent(context.Background(), service.Event{
		Time:      time.Now(),
		BreakerID: "payments",
		Type:      service.EventStateTransition,
		Before:    core.StateClosed,
		After:     core.StateOpen,
		State:     core.StateOpen,
		Outcome:   service.OutcomeFailure,
	})

	metricFamilies, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	if len(metricFamilies) == 0 {
		t.Fatal("expected gathered metrics")
	}

	assertMetricHasLabels(t, metricFamilies, "gcb_circuit_breaker_events_total", map[string]string{
		"breaker_id": "payments",
		"event_type": string(service.EventExecutionFinished),
		"state":      core.StateOpen.String(),
		"outcome":    string(service.OutcomeBlockedOpen),
	})
	assertMetricHasLabels(t, metricFamilies, "gcb_circuit_breaker_executions_total", map[string]string{
		"breaker_id": "payments",
		"state":      core.StateOpen.String(),
		"outcome":    string(service.OutcomeBlockedOpen),
	})
}

func TestObserverNotAutoRegistered(t *testing.T) {
	_ = NewObserver(DefaultConfig())

	metricFamilies, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather default metrics: %v", err)
	}

	for _, family := range metricFamilies {
		if family.GetName() == "gcb_circuit_breaker_events_total" ||
			family.GetName() == "gcb_circuit_breaker_executions_total" ||
			family.GetName() == "gcb_circuit_breaker_state" {
			t.Fatalf("metric %q should not be auto-registered", family.GetName())
		}
	}
}

func assertMetricHasLabels(t *testing.T, families []*dto.MetricFamily, metricName string, labels map[string]string) {
	t.Helper()

	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			actual := make(map[string]string, len(metric.GetLabel()))
			for _, pair := range metric.GetLabel() {
				actual[pair.GetName()] = pair.GetValue()
			}
			match := true
			for key, value := range labels {
				if actual[key] != value {
					match = false
					break
				}
			}
			if match {
				return
			}
		}
	}

	t.Fatalf("metric %q with labels %v not found", metricName, labels)
}
