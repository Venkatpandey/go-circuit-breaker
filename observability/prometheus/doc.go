// Package prometheus provides an optional Prometheus observer for service.Manager events.
//
// This package does not auto-register collectors. Callers can obtain collectors
// via Observer.Collectors() and register them with their own registry.
package prometheus
