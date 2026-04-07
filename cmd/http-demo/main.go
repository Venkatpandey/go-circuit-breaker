package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"go-circuit-breaker/core"
	"go-circuit-breaker/service"
)

func main() {
	ctx := context.Background()

	manager, err := service.NewManager(service.Options{
		BreakerConfig: core.Config{
			FailureThreshold: 3,
			SuccessThreshold: 2,
			CooldownPeriod:   2 * time.Second,
			RequestTimeout:   500 * time.Millisecond,
		},
	})
	if err != nil {
		panic(err)
	}

	server := newDemoServer()
	defer server.Close()

	client := &http.Client{}
	breakerID := "demo-http-upstream"

	fmt.Println("go-circuit-breaker HTTP demo")
	fmt.Println("The demo will trigger failures, trip open, recover through half-open, and close again.")
	fmt.Println()

	for attempt := 1; attempt <= 8; attempt++ {
		err := manager.Execute(ctx, breakerID, func(execCtx context.Context) error {
			req, err := http.NewRequestWithContext(execCtx, http.MethodGet, server.URL, nil)
			if err != nil {
				return err
			}

			resp, err := client.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode >= http.StatusInternalServerError {
				body, _ := io.ReadAll(resp.Body)
				return errors.New(string(body))
			}

			_, _ = io.ReadAll(resp.Body)
			return nil
		})

		stats, statsErr := manager.Stats(ctx, breakerID)
		if statsErr != nil {
			panic(statsErr)
		}

		fmt.Printf("attempt=%d result=%s state=%s failures=%d successes=%d probe_in_flight=%t\n",
			attempt,
			renderResult(err),
			stats.State,
			stats.ConsecutiveFailures,
			stats.ConsecutiveSuccesses,
			stats.HalfOpenProbeInFlight,
		)

		time.Sleep(700 * time.Millisecond)
	}
}

func renderResult(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, core.ErrCircuitOpen):
		return "blocked-open"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "failure"
	}
}

func newDemoServer() *httptest.Server {
	var requestCount int64

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt64(&requestCount, 1)

		switch {
		case current <= 3:
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
		case current == 4:
			time.Sleep(700 * time.Millisecond)
			http.Error(w, "slow recovery", http.StatusGatewayTimeout)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok"))
		}
	}))
}
