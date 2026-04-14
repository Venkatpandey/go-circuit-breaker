package prometheus

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
	"github.com/Venkatpandey/go-circuit-breaker/service"

	prom "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestDefaultMetricContract(t *testing.T) {
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
		Type:      service.EventExecutionFinished,
		State:     core.StateOpen,
		Outcome:   service.OutcomeBlockedOpen,
	})

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	assertMetricLabelKeys(t, families, "gcb_circuit_breaker_events_total", []string{
		"breaker_id", "event_type", "state", "outcome",
	})
	assertMetricLabelKeys(t, families, "gcb_circuit_breaker_executions_total", []string{
		"breaker_id", "state", "outcome",
	})
	assertMetricLabelKeys(t, families, "gcb_circuit_breaker_state", []string{
		"breaker_id", "state",
	})
}

func assertMetricLabelKeys(t *testing.T, families []*dto.MetricFamily, metricName string, expected []string) {
	t.Helper()

	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		metrics := family.GetMetric()
		if len(metrics) == 0 {
			t.Fatalf("metric %q was present with no samples", metricName)
		}

		got := make([]string, 0, len(metrics[0].GetLabel()))
		for _, label := range metrics[0].GetLabel() {
			got = append(got, label.GetName())
		}

		slices.Sort(got)
		sortedExpected := slices.Clone(expected)
		slices.Sort(sortedExpected)
		if !slices.Equal(got, sortedExpected) {
			t.Fatalf("metric %q labels mismatch: got=%v want=%v", metricName, got, sortedExpected)
		}
		return
	}

	t.Fatalf("metric %q not found", metricName)
}
