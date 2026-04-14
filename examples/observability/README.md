# Observability examples

This folder provides starter assets for monitoring `go-circuit-breaker` in Prometheus and Grafana.

## Files

- `prometheus-alerts.yml`: starter alert rules for blocked-open spikes and prolonged OPEN state.
- `grafana-dashboard.json`: starter dashboard showing event rate, execution outcomes, and state over time.

## Usage

1. Register the optional Prometheus observer in your service:

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

2. Load `prometheus-alerts.yml` into your Prometheus rule files.
3. Import `grafana-dashboard.json` into Grafana.
4. Adjust alert thresholds and dashboard variables to match your traffic patterns.

## Label cardinality reminder

Metrics include `breaker_id`, `state`, and `outcome`. Keep breaker IDs stable and bounded (for example by dependency name), not per user/request.

