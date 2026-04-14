# go-circuit-breaker

[![CI](https://github.com/Venkatpandey/go-circuit-breaker/actions/workflows/ci.yml/badge.svg)](https://github.com/Venkatpandey/go-circuit-breaker/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Venkatpandey/go-circuit-breaker)](https://goreportcard.com/report/github.com/Venkatpandey/go-circuit-breaker)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Venkatpandey/go-circuit-breaker)](https://github.com/Venkatpandey/go-circuit-breaker/blob/main/go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/Venkatpandey/go-circuit-breaker/blob/main/LICENSE)

`go-circuit-breaker` is a library-first Go implementation of the circuit breaker pattern for production services. It is designed for **local, in-process admission control** with an optional Redis adapter for snapshot persistence and operational introspection.

The project ships one runnable demo under `cmd/http-demo` and keeps Redis out of the hot path by default.

## Features

- ⚡ Fast local in-memory breaker path with low overhead.
- 🧠 Context-first execution API (`Execute(ctx, fn)`).
- 🔒 Strict half-open probe control (single probe in flight).
- 📡 Event hooks for transitions, outcomes, and internal lifecycle points.
- 📈 Optional Prometheus observer with explicit collector registration.
- 🧱 Optional Redis snapshot persistence with configurable write policy.
- 🧪 Unit, race, integration, and benchmark workflows.
- 📚 GoDoc-friendly exported API comments and package docs.

## Why local-first

The primary production model is one breaker per process protecting the dependency that process calls.

- Fast fail-open decisions stay in memory.
- Breaker behavior does not depend on Redis availability.
- Multi-instance race conditions are avoided because breaker state is not used as shared distributed coordination.

Redis is supported as an **optional adapter** for snapshots, state export, and demo/integration workflows. It is not positioned as a distributed breaker coordinator in v1.

## Packages

- `core`: breaker state machine, config, execution API, stats, snapshots
- `service`: named breaker manager and optional persistence policy
- `adapters`: optional integrations such as Redis snapshot storage
- `observability/prometheus`: optional Prometheus observer built on event hooks
- `cmd/http-demo`: runnable HTTP demo

## Get started

### Prerequisites

- Go 1.24 or newer
- Make
- Docker only if you want to run Redis locally for adapter experiments

### 1. Clone the repository

```bash
git clone git@github.com:Venkatpandey/go-circuit-breaker.git
cd go-circuit-breaker
```

### 2. Run the default verification suite

```bash
make test
make test-race
```

### 3. Run the HTTP demo

```bash
make demo
```

The demo shows:

- repeated upstream failures
- breaker opening
- requests blocked while open
- half-open recovery probes
- breaker closing again after recovery

### 4. Use the library in your project

Start with a local, in-memory manager and one breaker ID per dependency:

```go
package main

import (
	"context"
	"errors"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"
	"github.com/Venkatpandey/go-circuit-breaker/service"
)

func main() {
	manager, err := service.NewManager(service.Options{
		BreakerConfig: core.Config{
			FailureThreshold: 5,
			SuccessThreshold: 2,
			CooldownPeriod:   30 * time.Second,
			RequestTimeout:   2 * time.Second,
		},
	})
	if err != nil {
		panic(err)
	}

	_ = manager.Execute(context.Background(), "payments-api", func(ctx context.Context) error {
		// Your dependency call should honor ctx cancellation.
		return errors.New("upstream failed")
	})
}
```

### 5. Optional Redis-backed snapshots

If you want persistence or state inspection support:

```bash
make docker-up
make test-integration
```

## Public API shape

The public API is **context-first**.

- `(*core.CircuitBreaker).Execute(ctx, func(ctx context.Context) error)` is the convenience wrapper.
- `(*core.CircuitBreaker).Allow()` exposes lower-level admission control for advanced integrations.
- `(*service.Manager).Execute(ctx, id, fn)` manages named breakers without requiring any external store.

The library does not spawn goroutines around your operation. If you configure `RequestTimeout`, the library derives a child context and passes it to your function. Your dependency code must honor that context for cancellation to take effect.

## Observability

The manager supports synchronous event hooks so you can plug in metrics, logs, and alerts without forcing any telemetry dependency into the core hot path.

Event API highlights:

- `service.Observer` receives `service.Event` callbacks.
- Events include internal lifecycle points (`allow_granted`, `allow_denied`, `probe_started`, `execution_finished`, `state_transition`).
- Event payload includes breaker ID, state, before/after state, outcome, timestamp, and error.
- Observer failures are isolated from breaker logic. You can attach `ObserverErrHandler` to capture callback panics.

Minimal hook example:

```go
manager, err := service.NewManager(service.Options{
	BreakerConfig: core.DefaultConfig(),
	Observer: service.ObserverFunc(func(_ context.Context, event service.Event) {
		// Keep this callback fast and non-blocking.
		fmt.Printf("event=%s breaker=%s state=%s outcome=%s\n",
			event.Type, event.BreakerID, event.State, event.Outcome)
	}),
	ObserverErrHandler: func(err error) {
		// Optional: report observer callback failures.
	},
})
```

### Prometheus integration (optional)

Use `observability/prometheus` if you want ready-to-register collectors:

```go
obs := prometheusobs.NewObserver(prometheusobs.DefaultConfig())
for _, collector := range obs.Collectors() {
	prometheus.MustRegister(collector)
}

manager, err := service.NewManager(service.Options{
	BreakerConfig: core.DefaultConfig(),
	Observer:      obs,
})
```

Default metrics:

- `gcb_circuit_breaker_events_total{breaker_id,event_type,state,outcome}`
- `gcb_circuit_breaker_executions_total{breaker_id,state,outcome}`
- `gcb_circuit_breaker_state{breaker_id,state}`

Cardinality note:

- Labels include breaker ID, state, and outcome by default.
- Keep breaker IDs stable and bounded. Avoid per-user or per-request IDs.

Ready-to-use files:

- `examples/observability/prometheus-alerts.yml`
- `examples/observability/grafana-dashboard.json`
- `examples/observability/README.md`

Alert/dashboard ideas:

- Alert when execution `outcome="blocked_open"` rate spikes.
- Alert when `state="OPEN"` remains at `1` for longer than expected cooldown windows.
- Dashboard transition and outcome rates per breaker ID.

## Why use this over `sony/gobreaker`?

`sony/gobreaker` is one of the most widely used circuit breaker libraries in Go and is a good default choice when you want a very established package with broader policy knobs.

This project is a better fit when you specifically want:

- a **local-first production model** with no ambiguity around distributed breaker coordination
- a **context-first API** that makes cancellation part of the normal execution flow
- a **small, explicit surface area** that is easy to audit and integrate
- **optional persistence** for snapshots and inspection without putting Redis on the hot path
- a straightforward manager layer for **named in-process breakers**

`sony/gobreaker` is currently stronger if you need:

- more mature ecosystem adoption and battle-tested history
- richer trip-policy tuning out of the box
- broader hook/configuration support
- a more established default choice for teams that prefer convention over opinionated architecture

In short:

- Choose `go-circuit-breaker` if you want a simple, fast, context-aware, local-first breaker with clear persistence boundaries.
- Choose `sony/gobreaker` if you want the most established Go circuit breaker package with a longer production track record and more built-in policy flexibility.

This project does **not** try to claim feature parity with `sony/gobreaker` yet. Its value proposition is clarity, local-first architecture, and a clean hot path for modern Go services.

## Half-open behavior

The default half-open semantics are intentionally strict:

- After cooldown, the breaker transitions from `OPEN` to `HALF_OPEN`.
- Exactly one probe request is allowed at a time.
- Additional requests fail fast with `core.ErrCircuitOpen` while that probe is in flight.
- Successful probes close the breaker after `SuccessThreshold` consecutive successes.
- Any failed probe reopens the breaker immediately.

## Redis adapter

The Redis adapter stores serialized `core.Snapshot` values and is best used for:

- state inspection
- low-frequency persistence
- integration tests or demos

It is not intended to provide shared distributed breaker coordination.

Example:

```go
client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
store, err := adapters.NewRedisStore(client, adapters.DefaultRedisStoreConfig())
if err != nil {
	panic(err)
}

manager, err := service.NewManager(service.Options{
	BreakerConfig: core.DefaultConfig(),
	Store:         store,
	PersistPolicy: service.PersistOnStateChange,
})
```

Persistence policies:

- `service.PersistOnStateChange`: default and recommended
- `service.PersistAlways`: persist after every execution
- `service.PersistNever`: disable persistence entirely

## Build and development

```bash
make build
make test
make test-race
make benchmark
make demo
```

## GoDoc usage

Quick ways to use docs locally:

```bash
go doc github.com/Venkatpandey/go-circuit-breaker/core
go doc github.com/Venkatpandey/go-circuit-breaker/service
go doc github.com/Venkatpandey/go-circuit-breaker/service.Manager.Execute
```

Browse package docs in a local web UI:

```bash
go install golang.org/x/pkgsite/cmd/pkgsite@latest
pkgsite
```

Then open `http://localhost:8080/github.com/Venkatpandey/go-circuit-breaker`.

Optional Redis workflows:

```bash
make test-integration
```

If you want a real Redis instance for manual experimentation, you can also run:

```bash
make docker-up
```

## Operational guidance

- Keep breaker IDs bounded and meaningful. The manager cache is process-local and grows with distinct IDs until you delete them or restart the process.
- Prefer one breaker per dependency or dependency slice, not per request or per user.
- Use `PersistOnStateChange` unless you have a specific need for higher-frequency snapshots.
- Use request timeouts deliberately. The library can derive a timeout context, but your dependency call must honor it.
- For large Redis keyspaces, listing uses `SCAN`, not `KEYS`, to avoid blocking the server.

## Benchmarks

Run the local benchmark suite with:

```bash
make benchmark
```

Latest local benchmark sample:

- Environment: `darwin/arm64`, Apple M1
- Command: `go test -run=^$ -bench=. -benchmem ./core ./service`

```text
pkg: github.com/Venkatpandey/go-circuit-breaker/core
BenchmarkCircuitBreakerExecuteSuccess-8          4507650   254.9 ns/op   48 B/op   2 allocs/op
BenchmarkCircuitBreakerAllowCompleteSuccess-8    4879486   245.3 ns/op   48 B/op   2 allocs/op
BenchmarkCircuitBreakerOpenFastFail-8           10352617   115.7 ns/op    0 B/op   0 allocs/op
BenchmarkCircuitBreakerExecuteParallel-8         2014198   675.4 ns/op   48 B/op   2 allocs/op

pkg: github.com/Venkatpandey/go-circuit-breaker/service
BenchmarkManagerExecuteHotPath-8                 2052726   589.9 ns/op   48 B/op   2 allocs/op
BenchmarkManagerExecutePersistAlways-8           1855582   654.6 ns/op   48 B/op   2 allocs/op
BenchmarkManagerExecuteWithNoopObserver-8        1841437   650.0 ns/op   48 B/op   2 allocs/op
BenchmarkManagerExecuteWithPromObserver-8         891808  1333.0 ns/op   48 B/op   2 allocs/op
```

These numbers are intended as a reference point for the in-memory hot path. Actual results will vary by CPU, Go version, OS, and whether your production code adds network calls, tracing, logging, or persistence on top.

## Testing

- `make test`: unit tests for the library and manager layers
- `make test-race`: race detector across the default test suite
- `make test-integration`: Redis adapter tests behind the `integration` build tag
- `make benchmark`: local microbenchmarks for the core and manager hot paths

## Open source basics

- License: MIT, see `LICENSE`
- Contribution notes: see `CONTRIBUTING.md`
- CI: see `.github/workflows/ci.yml`
- Documentation style: exported APIs should include GoDoc comments and long files should use lightweight section headers.
