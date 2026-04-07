package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"go-circuit-breaker/core"

	"github.com/go-redis/redis/v8"
)

const (
	defaultRedisTimeout = 5 * time.Second
	defaultScanCount    = int64(100)
	defaultKeyPrefix    = "circuit_breaker:"
)

type RedisStoreConfig struct {
	Timeout   time.Duration
	KeyPrefix string
	ScanCount int64
}

func DefaultRedisStoreConfig() RedisStoreConfig {
	return RedisStoreConfig{
		Timeout:   defaultRedisTimeout,
		KeyPrefix: defaultKeyPrefix,
		ScanCount: defaultScanCount,
	}
}

type RedisStore struct {
	client *redis.Client
	config RedisStoreConfig
}

func NewRedisStore(client *redis.Client, config RedisStoreConfig) (*RedisStore, error) {
	if client == nil {
		return nil, errors.New("redis client cannot be nil")
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultRedisTimeout
	}
	if config.KeyPrefix == "" {
		config.KeyPrefix = defaultKeyPrefix
	}
	if config.ScanCount <= 0 {
		config.ScanCount = defaultScanCount
	}

	return &RedisStore{
		client: client,
		config: config,
	}, nil
}

func (r *RedisStore) Load(ctx context.Context, id string) (*core.Snapshot, error) {
	if id == "" {
		return nil, errors.New("circuit breaker id cannot be empty")
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	raw, err := r.client.Get(ctx, r.key(id)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("load redis snapshot: %w", err)
	}

	var snapshot core.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode redis snapshot: %w", err)
	}

	return &snapshot, nil
}

func (r *RedisStore) Save(ctx context.Context, id string, snapshot core.Snapshot) error {
	if id == "" {
		return errors.New("circuit breaker id cannot be empty")
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode redis snapshot: %w", err)
	}

	if err := r.client.Set(ctx, r.key(id), payload, 0).Err(); err != nil {
		return fmt.Errorf("save redis snapshot: %w", err)
	}

	return nil
}

func (r *RedisStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return errors.New("circuit breaker id cannot be empty")
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	if err := r.client.Del(ctx, r.key(id)).Err(); err != nil {
		return fmt.Errorf("delete redis snapshot: %w", err)
	}

	return nil
}

func (r *RedisStore) List(ctx context.Context) ([]string, error) {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	var cursor uint64
	ids := make([]string, 0)
	pattern := r.config.KeyPrefix + "*"

	for {
		keys, nextCursor, err := r.client.Scan(ctx, cursor, pattern, r.config.ScanCount).Result()
		if err != nil {
			return nil, fmt.Errorf("scan redis keys: %w", err)
		}
		for _, key := range keys {
			ids = append(ids, strings.TrimPrefix(key, r.config.KeyPrefix))
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return ids, nil
}

func (r *RedisStore) Ping(ctx context.Context) error {
	ctx, cancel := r.withTimeout(ctx)
	defer cancel()
	return r.client.Ping(ctx).Err()
}

func (r *RedisStore) Clear(ctx context.Context) error {
	ids, err := r.List(ctx)
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	keys := make([]string, 0, len(ids))
	for _, id := range ids {
		keys = append(keys, r.key(id))
	}
	return r.client.Del(ctx, keys...).Err()
}

func (r *RedisStore) Exists(ctx context.Context, id string) (bool, error) {
	if id == "" {
		return false, errors.New("circuit breaker id cannot be empty")
	}

	ctx, cancel := r.withTimeout(ctx)
	defer cancel()

	count, err := r.client.Exists(ctx, r.key(id)).Result()
	if err != nil {
		return false, fmt.Errorf("check redis snapshot existence: %w", err)
	}
	return count > 0, nil
}

func (r *RedisStore) key(id string) string {
	return r.config.KeyPrefix + id
}

func (r *RedisStore) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithTimeout(ctx, r.config.Timeout)
}
