package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-circuit-breaker/core"

	"github.com/go-redis/redis/v8"
)

type RedisRepository struct {
	client *redis.Client
}

func NewRedisRepository(client *redis.Client) *RedisRepository {
	return &RedisRepository{client: client}
}

func (r *RedisRepository) Save(cb *core.CircuitBreaker) error {
	ctx := context.Background()
	data, err := json.Marshal(cb)
	if err != nil {
		return fmt.Errorf("failed to marshal circuit breaker state: %w", err)
	}
	key := "circuit_breaker:default"
	err = r.client.Set(ctx, key, data, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to save circuit breaker state to Redis: %w", err)
	}
	return nil
}

func (r *RedisRepository) FindByID(id string) (*core.CircuitBreaker, error) {
	ctx := context.Background()
	key := fmt.Sprintf("circuit_breaker:%s", id)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to retrieve circuit breaker state from Redis: %w", err)
	}
	var cb core.CircuitBreaker
	err = json.Unmarshal(data, &cb)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal circuit breaker state: %w", err)
	}
	return &cb, nil
}
