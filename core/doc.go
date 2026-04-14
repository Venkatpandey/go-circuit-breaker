// Package core contains the in-process circuit breaker state machine.
//
// The core package is dependency-free and focused on deterministic breaker
// behavior: state transitions, admission checks, execution completion handling,
// and snapshot/restore support.
package core
