package service

import "testing"

func TestEventTypeContract(t *testing.T) {
	tests := []struct {
		name string
		got  EventType
		want EventType
	}{
		{name: "allow granted", got: EventAllowGranted, want: "allow_granted"},
		{name: "allow denied", got: EventAllowDenied, want: "allow_denied"},
		{name: "probe started", got: EventProbeStarted, want: "probe_started"},
		{name: "execution finished", got: EventExecutionFinished, want: "execution_finished"},
		{name: "state transition", got: EventStateTransition, want: "state_transition"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s contract mismatch: got=%q want=%q", tt.name, tt.got, tt.want)
		}
	}
}

func TestOutcomeContract(t *testing.T) {
	tests := []struct {
		name string
		got  Outcome
		want Outcome
	}{
		{name: "unknown", got: OutcomeUnknown, want: "unknown"},
		{name: "success", got: OutcomeSuccess, want: "success"},
		{name: "failure", got: OutcomeFailure, want: "failure"},
		{name: "blocked open", got: OutcomeBlockedOpen, want: "blocked_open"},
		{name: "timeout", got: OutcomeTimeout, want: "timeout"},
		{name: "canceled", got: OutcomeCanceled, want: "canceled"},
		{name: "deadline exceeded", got: OutcomeDeadlineExceeded, want: "deadline_exceeded"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Fatalf("%s contract mismatch: got=%q want=%q", tt.name, tt.got, tt.want)
		}
	}
}
