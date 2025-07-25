package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-circuit-breaker/core"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	circuitBreakerKeyPrefix = "circuit_breaker:"
	defaultTimeout          = 5 * time.Second
	keyPattern              = circuitBreakerKeyPrefix + "*"
)

// RedisConfig holds configuration for Redis repository
type RedisConfig struct {
	Timeout time.Duration
	Context context.Context
}

// DefaultRedisConfig returns sensible defaults
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Timeout: defaultTimeout,
		Context: context.Background(),
	}
}

type RedisRepository struct {
	client *redis.Client
	config RedisConfig
}

func NewRedisRepository(client *redis.Client) *RedisRepository {
	return NewRedisRepositoryWithConfig(client, DefaultRedisConfig())
}

func NewRedisRepositoryWithConfig(client *redis.Client, config RedisConfig) *RedisRepository {
	if client == nil {
		panic("redis client cannot be nil")
	}

	return &RedisRepository{
		client: client,
		config: config,
	}
}

// Save stores a circuit breaker with the given ID
func (r *RedisRepository) Save(id string, cb *core.CircuitBreaker) error {
	if id == "" {
		return errors.New("circuit breaker ID cannot be empty")
	}
	if cb == nil {
		return errors.New("circuit breaker cannot be nil")
	}

	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	// Create a serializable version of the circuit breaker
	cbData := r.circuitBreakerToData(cb)

	data, err := json.Marshal(cbData)
	if err != nil {
		return fmt.Errorf("failed to marshal circuit breaker state: %w", err)
	}

	key := r.buildKey(id)
	err = r.client.Set(ctx, key, data, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to save circuit breaker state to Redis (key: %s): %w", key, err)
	}

	return nil
}

// FindByID retrieves a circuit breaker by ID
func (r *RedisRepository) FindByID(id string) (*core.CircuitBreaker, error) {
	if id == "" {
		return nil, errors.New("circuit breaker ID cannot be empty")
	}

	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	key := r.buildKey(id)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // Not found
		}
		return nil, fmt.Errorf("failed to retrieve circuit breaker state from Redis (key: %s): %w", key, err)
	}

	var cbData circuitBreakerData
	err = json.Unmarshal(data, &cbData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal circuit breaker state (key: %s): %w", key, err)
	}

	cb, err := r.dataToCircuitBreaker(cbData)
	if err != nil {
		return nil, fmt.Errorf("failed to reconstruct circuit breaker (key: %s): %w", key, err)
	}

	return cb, nil
}

// Delete removes a circuit breaker by ID
func (r *RedisRepository) Delete(id string) error {
	if id == "" {
		return errors.New("circuit breaker ID cannot be empty")
	}

	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	key := r.buildKey(id)
	result := r.client.Del(ctx, key)

	if err := result.Err(); err != nil {
		return fmt.Errorf("failed to delete circuit breaker from Redis (key: %s): %w", key, err)
	}

	// Check if the key actually existed
	if result.Val() == 0 {
		return fmt.Errorf("circuit breaker not found (key: %s)", key)
	}

	return nil
}

// List returns all circuit breaker IDs
func (r *RedisRepository) List() ([]string, error) {
	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	keys, err := r.client.Keys(ctx, keyPattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list circuit breaker keys from Redis: %w", err)
	}

	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		id := r.extractIDFromKey(key)
		if id != "" {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// Exists checks if a circuit breaker exists
func (r *RedisRepository) Exists(id string) (bool, error) {
	if id == "" {
		return false, errors.New("circuit breaker ID cannot be empty")
	}

	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	key := r.buildKey(id)
	result := r.client.Exists(ctx, key)

	if err := result.Err(); err != nil {
		return false, fmt.Errorf("failed to check circuit breaker existence (key: %s): %w", key, err)
	}

	return result.Val() > 0, nil
}

// Clear removes all circuit breakers (useful for testing)
func (r *RedisRepository) Clear() error {
	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	keys, err := r.client.Keys(ctx, keyPattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get circuit breaker keys for clearing: %w", err)
	}

	if len(keys) == 0 {
		return nil // Nothing to clear
	}

	err = r.client.Del(ctx, keys...).Err()
	if err != nil {
		return fmt.Errorf("failed to clear circuit breakers from Redis: %w", err)
	}

	return nil
}

// Ping checks Redis connectivity
func (r *RedisRepository) Ping() error {
	ctx, cancel := r.getContextWithTimeout()
	defer cancel()

	return r.client.Ping(ctx).Err()
}

// Helper methods

func (r *RedisRepository) getContextWithTimeout() (context.Context, context.CancelFunc) {
	if r.config.Context != nil {
		return context.WithTimeout(r.config.Context, r.config.Timeout)
	}
	return context.WithTimeout(context.Background(), r.config.Timeout)
}

func (r *RedisRepository) buildKey(id string) string {
	return circuitBreakerKeyPrefix + id
}

func (r *RedisRepository) extractIDFromKey(key string) string {
	if !strings.HasPrefix(key, circuitBreakerKeyPrefix) {
		return ""
	}
	return strings.TrimPrefix(key, circuitBreakerKeyPrefix)
}

// Serialization structs and methods

// circuitBreakerData represents the serializable version of CircuitBreaker
type circuitBreakerData struct {
	State            int   `json:"state"`
	FailureThreshold int   `json:"failure_threshold"`
	SuccessThreshold int   `json:"success_threshold"`
	FailureCount     int   `json:"failure_count"`
	SuccessCount     int   `json:"success_count"`
	Timeout          int64 `json:"timeout_ms"`         // Store as milliseconds
	LastFailureTime  int64 `json:"last_failure_time"`  // Store as Unix timestamp
	CooldownPeriod   int64 `json:"cooldown_period_ms"` // Store as milliseconds
}

func (r *RedisRepository) circuitBreakerToData(cb *core.CircuitBreaker) circuitBreakerData {
	stats := cb.GetStats()
	failureThreshold, successThreshold, timeout, cooldownPeriod := cb.GetConfiguration()

	return circuitBreakerData{
		State:            int(stats.State),
		FailureThreshold: failureThreshold,
		SuccessThreshold: successThreshold,
		FailureCount:     stats.FailureCount,
		SuccessCount:     stats.SuccessCount,
		Timeout:          timeout.Milliseconds(),
		LastFailureTime:  stats.LastFailure.Unix(),
		CooldownPeriod:   cooldownPeriod.Milliseconds(),
	}
}

func (r *RedisRepository) dataToCircuitBreaker(data circuitBreakerData) (*core.CircuitBreaker, error) {
	timeout := time.Duration(data.Timeout) * time.Millisecond
	cooldown := time.Duration(data.CooldownPeriod) * time.Millisecond

	cb, err := core.NewCircuitBreaker(
		data.FailureThreshold,
		data.SuccessThreshold,
		timeout,
		cooldown,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create circuit breaker from data: %w", err)
	}

	// Restore the saved state
	lastFailure := time.Unix(data.LastFailureTime, 0)
	err = cb.RestoreState(
		core.State(data.State),
		data.FailureCount,
		data.SuccessCount,
		lastFailure,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to restore circuit breaker state: %w", err)
	}

	return cb, nil
}
