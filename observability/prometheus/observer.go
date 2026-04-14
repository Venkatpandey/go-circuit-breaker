package prometheus

import (
	"context"
	"sync"

	"github.com/Venkatpandey/go-circuit-breaker/service"

	prom "github.com/prometheus/client_golang/prometheus"
)

// Config customizes Prometheus metric names and labels.
type Config struct {
	Namespace   string
	Subsystem   string
	ConstLabels prom.Labels
}

// DefaultConfig returns the default metric namespace/subsystem.
func DefaultConfig() Config {
	return Config{
		Namespace: "gcb",
		Subsystem: "circuit_breaker",
	}
}

// Observer exposes circuit breaker metrics and implements service.Observer.
type Observer struct {
	eventsTotal     *prom.CounterVec
	executionsTotal *prom.CounterVec
	stateGauge      *prom.GaugeVec

	mu           sync.Mutex
	stateByBreak map[string]string
}

// NewObserver creates an observer with unregistered collectors.
func NewObserver(cfg Config) *Observer {
	eventsTotal := prom.NewCounterVec(prom.CounterOpts{
		Namespace:   cfg.Namespace,
		Subsystem:   cfg.Subsystem,
		Name:        "events_total",
		Help:        "Total number of circuit breaker events.",
		ConstLabels: cfg.ConstLabels,
	}, []string{"breaker_id", "event_type", "state", "outcome"})

	executionsTotal := prom.NewCounterVec(prom.CounterOpts{
		Namespace:   cfg.Namespace,
		Subsystem:   cfg.Subsystem,
		Name:        "executions_total",
		Help:        "Total number of circuit breaker execution outcomes.",
		ConstLabels: cfg.ConstLabels,
	}, []string{"breaker_id", "state", "outcome"})

	stateGauge := prom.NewGaugeVec(prom.GaugeOpts{
		Namespace:   cfg.Namespace,
		Subsystem:   cfg.Subsystem,
		Name:        "state",
		Help:        "Current circuit breaker state. Exactly one state label should be 1 for each breaker.",
		ConstLabels: cfg.ConstLabels,
	}, []string{"breaker_id", "state"})

	return &Observer{
		eventsTotal:     eventsTotal,
		executionsTotal: executionsTotal,
		stateGauge:      stateGauge,
		stateByBreak:    make(map[string]string),
	}
}

// Collectors returns observer collectors for explicit registration.
func (o *Observer) Collectors() []prom.Collector {
	return []prom.Collector{
		o.eventsTotal,
		o.executionsTotal,
		o.stateGauge,
	}
}

// OnCircuitBreakerEvent implements service.Observer.
func (o *Observer) OnCircuitBreakerEvent(_ context.Context, event service.Event) {
	breakerID := event.BreakerID
	state := event.State.String()
	outcome := string(event.Outcome)
	if outcome == "" {
		outcome = string(service.OutcomeUnknown)
	}

	o.eventsTotal.WithLabelValues(breakerID, string(event.Type), state, outcome).Inc()

	if event.Type == service.EventExecutionFinished {
		o.executionsTotal.WithLabelValues(breakerID, state, outcome).Inc()
	}

	o.updateStateGauge(breakerID, state)
}

func (o *Observer) updateStateGauge(breakerID, currentState string) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if prevState, ok := o.stateByBreak[breakerID]; ok && prevState != currentState {
		o.stateGauge.WithLabelValues(breakerID, prevState).Set(0)
	}

	o.stateByBreak[breakerID] = currentState
	o.stateGauge.WithLabelValues(breakerID, currentState).Set(1)
}
