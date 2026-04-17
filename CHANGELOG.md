# Changelog

All notable changes to this project are documented in this file.

The format is based on Keep a Changelog and this project follows Semantic Versioning.

## [Unreleased]

### Added
- Public stability policy and deprecation guidance in README.
- Go support policy (current + previous stable versions) in README/RELEASING.
- Release process documentation in `RELEASING.md`.
- CI quality gates for `go vet`, `staticcheck`, and `govulncheck`.
- Release workflow for validated manual tagging and GitHub release creation.
- Contract tests for exported event/outcome names and Prometheus metric label schemas.

## [v1.0.0] - 2026-04-14

### Added
- Local-first circuit breaker core with context-first execution API.
- Named manager with optional snapshot persistence policy.
- Event hook API with state transition and execution lifecycle events.
- Optional Prometheus observer integration.
- Redis snapshot adapter and integration tests.
- Observability examples for Grafana and Prometheus alerts.
- Benchmarks for core and manager hot paths.

### Changed
- Module path set to `github.com/Venkatpandey/go-circuit-breaker`.
- Documentation expanded with GoDoc usage, observability guidance, and release contracts.

### Stability
- This release establishes the first stable `v1` API and telemetry contract baseline.
