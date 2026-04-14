// Package service provides the named circuit breaker manager and observability hooks.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
)

// EventType identifies the emitted event category.
type EventType string

const (
	// EventAllowGranted is emitted when a request is admitted by the breaker.
	EventAllowGranted EventType = "allow_granted"
	// EventAllowDenied is emitted when a request is rejected by the breaker.
	EventAllowDenied EventType = "allow_denied"
	// EventProbeStarted is emitted when a half-open probe begins.
	EventProbeStarted EventType = "probe_started"
	// EventExecutionFinished is emitted after a user function returns.
	EventExecutionFinished EventType = "execution_finished"
	// EventStateTransition is emitted when breaker state changes.
	EventStateTransition EventType = "state_transition"
)

// Outcome identifies execution result semantics.
type Outcome string

const (
	OutcomeUnknown          Outcome = "unknown"
	OutcomeSuccess          Outcome = "success"
	OutcomeFailure          Outcome = "failure"
	OutcomeBlockedOpen      Outcome = "blocked_open"
	OutcomeTimeout          Outcome = "timeout"
	OutcomeCanceled         Outcome = "canceled"
	OutcomeDeadlineExceeded Outcome = "deadline_exceeded"
)

// Event describes one breaker lifecycle event.
type Event struct {
	Time      time.Time
	BreakerID string
	Type      EventType
	Before    core.State
	After     core.State
	State     core.State
	Outcome   Outcome
	Err       error
}

// Observer consumes breaker events.
type Observer interface {
	OnCircuitBreakerEvent(ctx context.Context, event Event)
}

// ObserverFunc is a function adapter for Observer.
type ObserverFunc func(ctx context.Context, event Event)

// OnCircuitBreakerEvent implements Observer.
func (f ObserverFunc) OnCircuitBreakerEvent(ctx context.Context, event Event) {
	f(ctx, event)
}

// ObserverError represents an observer callback failure.
type ObserverError struct {
	ObserverIndex int
	EventType     EventType
	Cause         error
}

// Error implements error.
func (e *ObserverError) Error() string {
	return fmt.Sprintf("observer %d failed while handling %q: %v", e.ObserverIndex, e.EventType, e.Cause)
}

// Unwrap implements errors.Wrapper.
func (e *ObserverError) Unwrap() error {
	return e.Cause
}

// ObserverErrorHandler receives observer callback errors.
type ObserverErrorHandler func(err error)

// ComposeObservers combines multiple observers into one.
func ComposeObservers(observers ...Observer) Observer {
	filtered := make([]Observer, 0, len(observers))
	for _, observer := range observers {
		if observer != nil {
			filtered = append(filtered, observer)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return ObserverFunc(func(ctx context.Context, event Event) {
		for _, observer := range filtered {
			observer.OnCircuitBreakerEvent(ctx, event)
		}
	})
}
