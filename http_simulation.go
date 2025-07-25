package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
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
			failureThreshold: 5,
			resetTimeout:     30 * time.Second,
			state:            "closed",
		},
		retryConfig: RetryConfig{
			MaxAttempts: 3,
			BaseDelay:   100 * time.Millisecond,
			MaxDelay:    5 * time.Second,
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
		return nil, fmt.Errorf("circuit breaker is open")
	}

	var lastErr error
	for attempt := 1; attempt <= c.retryConfig.MaxAttempts; attempt++ {
		// Execute the operation
		resp, err := operation()

		// Handle success
		if err == nil && c.isSuccessResponse(resp) {
			c.circuitBreaker.recordSuccess()
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

		fmt.Printf("Attempt %d failed: %v. Retrying in %v...\n", attempt, lastErr, delay)

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
	cb.failures = 0
	cb.state = "closed"
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	if cb.failures >= cb.failureThreshold {
		cb.state = "open"
	}
}

// Example usage functions

// APIResponse represents a typical API response
type APIResponse struct {
	ID      int    `json:"id"`
	Title   string `json:"title"`
	Message string `json:"message"`
}

// fetchUserData demonstrates fetching data from an API
func fetchUserData(client *HTTPClient, userID int) (*APIResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://jsonplaceholder.typicode.com/posts/%d", userID)

	resp, err := client.Get(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user data: %w", err)
	}
	defer resp.Body.Close()

	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &apiResp, nil
}

// postUserData demonstrates posting data to an API
func postUserData(client *HTTPClient, data APIResponse) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	url := "https://jsonplaceholder.typicode.com/posts"
	resp, err := client.Post(ctx, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to post data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// Example integration in main function
func main() {
	// Create HTTP client with resilience patterns
	client := NewHTTPClient(5 * time.Second)

	// Example 1: Fetch data with retry and circuit breaker
	fmt.Println("=== Fetching User Data ===")
	userData, err := fetchUserData(client, 1)
	if err != nil {
		fmt.Printf("Error fetching user data: %v\n", err)
	} else {
		fmt.Printf("User data: %+v\n", userData)
	}

	// Example 2: Post data with resilience
	fmt.Println("\n=== Posting User Data ===")
	newData := APIResponse{
		Title:   "Test Post",
		Message: "This is a test message",
	}

	if err := postUserData(client, newData); err != nil {
		fmt.Printf("Error posting data: %v\n", err)
	} else {
		fmt.Println("Data posted successfully")
	}

	// Example 3: Simulate failures to test circuit breaker
	fmt.Println("\n=== Testing Circuit Breaker ===")
	for i := 0; i < 8; i++ { // This will trigger circuit breaker
		_, err := fetchUserData(client, 999) // Non-existent resource
		if err != nil {
			fmt.Printf("Request %d failed: %v\n", i+1, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
