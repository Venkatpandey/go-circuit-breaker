package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

// HTTPClient wraps http.Client with circuit breaker and retry logic
type HTTPClient struct {
	client         *http.Client
	circuitBreaker *CircuitBreaker
	retryConfig    RetryConfig
}

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// CircuitBreaker represents a simple circuit breaker
type CircuitBreaker struct {
	failureThreshold int
	resetTimeout     time.Duration
	failures         int
	lastFailureTime  time.Time
	state            string // "closed", "open", "half-open"
}

// NewHTTPClient creates a new HTTP client with resilience patterns
func NewHTTPClient(timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		circuitBreaker: &CircuitBreaker{
			failureThreshold: 3,               // Reduced for demo
			resetTimeout:     5 * time.Second, // Reduced for demo
			state:            "closed",
		},
		retryConfig: RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   200 * time.Millisecond,
			MaxDelay:    2 * time.Second,
		},
	}
}

// Get performs a GET request with circuit breaker and retry logic
func (c *HTTPClient) Get(ctx context.Context, url string) (*http.Response, error) {
	return c.doWithResilience(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, err
		}
		return c.client.Do(req)
	})
}

// Post performs a POST request with circuit breaker and retry logic
func (c *HTTPClient) Post(ctx context.Context, url string, body io.Reader) (*http.Response, error) {
	return c.doWithResilience(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", url, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		return c.client.Do(req)
	})
}

// doWithResilience executes HTTP requests with circuit breaker and retry
func (c *HTTPClient) doWithResilience(ctx context.Context, operation func() (*http.Response, error)) (*http.Response, error) {
	// Check circuit breaker state
	if !c.circuitBreaker.canExecute() {
		return nil, fmt.Errorf("circuit breaker is OPEN - requests blocked")
	}

	var lastErr error
	for attempt := 1; attempt <= c.retryConfig.MaxAttempts; attempt++ {
		// Execute the operation
		resp, err := operation()

		// Handle success
		if err == nil && c.isSuccessResponse(resp) {
			c.circuitBreaker.recordSuccess()
			if attempt > 1 {
				fmt.Printf("✅ Request succeeded on attempt %d\n", attempt)
			}
			return resp, nil
		}

		// Handle failure
		lastErr = err
		if resp != nil {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
			resp.Body.Close()
		}

		c.circuitBreaker.recordFailure()

		// Don't retry on last attempt
		if attempt == c.retryConfig.MaxAttempts {
			break
		}

		// Calculate retry delay with exponential backoff
		delay := c.calculateDelay(attempt)

		fmt.Printf("❌ Attempt %d failed: %v. Retrying in %v... (CB failures: %d/%d, state: %s)\n",
			attempt, lastErr, delay, c.circuitBreaker.failures, c.circuitBreaker.failureThreshold, c.circuitBreaker.state)

		// Wait for retry delay or context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
			// Continue to next attempt
		}
	}

	return nil, fmt.Errorf("all %d attempts failed, last error: %v", c.retryConfig.MaxAttempts, lastErr)
}

// isSuccessResponse checks if the HTTP response indicates success
func (c *HTTPClient) isSuccessResponse(resp *http.Response) bool {
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// calculateDelay calculates retry delay with exponential backoff and jitter
func (c *HTTPClient) calculateDelay(attempt int) time.Duration {
	// Exponential backoff: baseDelay * 2^(attempt-1)
	delay := time.Duration(attempt-1) * c.retryConfig.BaseDelay * 2

	// Cap at max delay
	if delay > c.retryConfig.MaxDelay {
		delay = c.retryConfig.MaxDelay
	}

	// Add jitter (±25%)
	jitter := time.Duration(float64(delay) * 0.25 * (2*rand.Float64() - 1))
	delay += jitter

	if delay < 0 {
		delay = c.retryConfig.BaseDelay
	}

	return delay
}

// Circuit Breaker Methods
func (cb *CircuitBreaker) canExecute() bool {
	now := time.Now()

	switch cb.state {
	case "closed":
		return true
	case "open":
		if now.Sub(cb.lastFailureTime) > cb.resetTimeout {
			cb.state = "half-open"
			fmt.Printf("🔄 Circuit breaker transitioning to HALF-OPEN\n")
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return true
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	if cb.state == "half-open" {
		fmt.Printf("✅ Circuit breaker reset to CLOSED after successful request\n")
	}
	cb.failures = 0
	cb.state = "closed"
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.failureThreshold && cb.state != "open" {
		cb.state = "open"
		fmt.Printf("🚫 Circuit breaker OPENED after %d failures\n", cb.failures)
	}
}

// APIResponse represents a typical API response
type APIResponse struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// MockService simulates an unreliable external service
type MockService struct {
	server       *httptest.Server
	requestCount int64
	failureRate  float64 // 0.0 to 1.0
	latencyMs    int
	recoverAfter int // Recover after N requests
}

// NewMockService creates a new mock HTTP service
func NewMockService(failureRate float64, latencyMs int, recoverAfter int) *MockService {
	ms := &MockService{
		failureRate:  failureRate,
		latencyMs:    latencyMs,
		recoverAfter: recoverAfter,
	}

	mux := http.NewServeMux()

	// GET endpoint
	mux.HandleFunc("/api/data/", ms.handleGet)

	// POST endpoint
	mux.HandleFunc("/api/data", ms.handlePost)

	ms.server = httptest.NewServer(mux)
	return ms
}

func (ms *MockService) handleGet(w http.ResponseWriter, r *http.Request) {
	reqNum := atomic.AddInt64(&ms.requestCount, 1)

	// Add latency
	if ms.latencyMs > 0 {
		time.Sleep(time.Duration(ms.latencyMs) * time.Millisecond)
	}

	// Simulate recovery after N requests
	shouldFail := false
	if ms.recoverAfter > 0 && reqNum <= int64(ms.recoverAfter) {
		shouldFail = rand.Float64() < ms.failureRate
	} else if ms.recoverAfter > 0 {
		// Service has "recovered"
		shouldFail = false
	} else {
		shouldFail = rand.Float64() < ms.failureRate
	}

	fmt.Printf("🌐 Mock service received GET request #%d (shouldFail: %v)\n", reqNum, shouldFail)

	if shouldFail {
		// Random error responses
		errors := []int{500, 502, 503, 504, 429}
		statusCode := errors[rand.Intn(len(errors))]
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Service temporarily unavailable (status: %d)", statusCode),
		})
		return
	}

	// Success response
	response := APIResponse{
		ID:      int(reqNum),
		Title:   fmt.Sprintf("Data Item %d", reqNum),
		Message: "Successfully retrieved data",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (ms *MockService) handlePost(w http.ResponseWriter, r *http.Request) {
	reqNum := atomic.AddInt64(&ms.requestCount, 1)

	// Add latency
	if ms.latencyMs > 0 {
		time.Sleep(time.Duration(ms.latencyMs) * time.Millisecond)
	}

	shouldFail := rand.Float64() < ms.failureRate

	fmt.Printf("🌐 Mock service received POST request #%d (shouldFail: %v)\n", reqNum, shouldFail)

	if shouldFail {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Internal server error during POST",
		})
		return
	}

	// Success response
	w.WriteHeader(201)
	response := APIResponse{
		ID:      int(reqNum),
		Title:   "Created successfully",
		Message: "Data posted successfully",
	}
	json.NewEncoder(w).Encode(response)
}

func (ms *MockService) URL() string {
	return ms.server.URL
}

func (ms *MockService) Close() {
	ms.server.Close()
}

func (ms *MockService) SetFailureRate(rate float64) {
	ms.failureRate = rate
}

func (ms *MockService) GetRequestCount() int64 {
	return atomic.LoadInt64(&ms.requestCount)
}

// Test functions
func testSuccessfulRequests(client *HTTPClient, baseURL string) {
	fmt.Println("\n" + "===========================================")
	fmt.Println("TEST 1: Successful Requests (Low failure rate)")
	fmt.Println("===========================================")

	for i := 1; i <= 5; i++ {
		fmt.Printf("\n--- Request %d ---\n", i)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		resp, err := client.Get(ctx, baseURL+"/api/data/"+fmt.Sprint(i))
		if err != nil {
			fmt.Printf("❌ Request failed: %v\n", err)
		} else {
			var result APIResponse
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			fmt.Printf("✅ Success: %+v\n", result)
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
}

func testHighFailureRate(client *HTTPClient, mockService *MockService) {
	fmt.Println("\n" + "===========================================")
	fmt.Println("TEST 2: High Failure Rate (Circuit Breaker Activation)")
	fmt.Println("===========================================")

	// Increase failure rate to trigger circuit breaker
	mockService.SetFailureRate(0.8)

	for i := 1; i <= 8; i++ {
		fmt.Printf("\n--- Request %d ---\n", i)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		resp, err := client.Get(ctx, mockService.URL()+"/api/data/"+fmt.Sprint(i))
		if err != nil {
			fmt.Printf("❌ Request failed: %v\n", err)
		} else {
			var result APIResponse
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			fmt.Printf("✅ Success: %+v\n", result)
		}
		cancel()
		time.Sleep(1 * time.Second)
	}
}

func testRecovery(client *HTTPClient, mockService *MockService) {
	fmt.Println("\n" + "===========================================")
	fmt.Println("TEST 3: Service Recovery (Circuit Breaker Reset)")
	fmt.Println("===========================================")

	// Simulate service recovery
	mockService.SetFailureRate(0.0)

	fmt.Println("⏳ Waiting for circuit breaker reset timeout...")
	time.Sleep(6 * time.Second) // Wait for circuit breaker reset

	for i := 1; i <= 3; i++ {
		fmt.Printf("\n--- Recovery Request %d ---\n", i)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		resp, err := client.Get(ctx, mockService.URL()+"/api/data/recovery"+fmt.Sprint(i))
		if err != nil {
			fmt.Printf("❌ Request failed: %v\n", err)
		} else {
			var result APIResponse
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			fmt.Printf("✅ Success: %+v\n", result)
		}
		cancel()
		time.Sleep(1 * time.Second)
	}
}

func testPostRequests(client *HTTPClient, baseURL string) {
	fmt.Println("\n" + "===========================================")
	fmt.Println("TEST 4: POST Requests with Resilience")
	fmt.Println("===========================================")

	for i := 1; i <= 3; i++ {
		fmt.Printf("\n--- POST Request %d ---\n", i)

		data := APIResponse{
			Title:   fmt.Sprintf("Test Post %d", i),
			Message: fmt.Sprintf("This is test message %d", i),
		}

		jsonData, _ := json.Marshal(data)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

		resp, err := client.Post(ctx, baseURL+"/api/data", bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("❌ POST failed: %v\n", err)
		} else {
			var result APIResponse
			json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			fmt.Printf("✅ POST Success: %+v\n", result)
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
}

func main() {
	fmt.Println("🚀 Starting HTTP Client Resilience Simulation")
	fmt.Println("This demo shows circuit breaker, retry logic, and error handling")

	// Create HTTP client with resilience patterns
	client := NewHTTPClient(2 * time.Second)

	// Create mock service with 30% failure rate, 100ms latency, recover after 15 requests
	mockService := NewMockService(0.3, 100, 15)
	defer mockService.Close()

	fmt.Printf("🌐 Mock service running at: %s\n", mockService.URL())

	// Run test scenarios
	testSuccessfulRequests(client, mockService.URL())
	testHighFailureRate(client, mockService)
	testRecovery(client, mockService)
	testPostRequests(client, mockService.URL())

	// Final statistics
	fmt.Println("\n" + "===========================================")
	fmt.Println("SIMULATION COMPLETE")
	fmt.Println("===================================================")
	fmt.Printf("📊 Total requests processed by mock service: %d\n", mockService.GetRequestCount())
	fmt.Printf("🔧 Circuit breaker final state: %s\n", client.circuitBreaker.state)
	fmt.Printf("📈 Circuit breaker failure count: %d/%d\n",
		client.circuitBreaker.failures, client.circuitBreaker.failureThreshold)
}
