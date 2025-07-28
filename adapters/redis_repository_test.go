package adapters

import (
	"fmt"
	"go-circuit-breaker/core"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test utilities
func setupTestRedis(t *testing.T) (*RedisRepository, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	require.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	repo := NewRedisRepository(client)

	t.Cleanup(func() {
		client.Close()
		mr.Close()
	})

	return repo, mr
}

func createTestCircuitBreaker(t *testing.T) *core.CircuitBreaker {
	cb, err := core.NewCircuitBreaker(3, 2, time.Second, 5*time.Second)
	require.NoError(t, err)
	return cb
}

// Save operation tests
func TestRedisRepository_Save(t *testing.T) {
	tests := []struct {
		name           string
		id             string
		circuitBreaker *core.CircuitBreaker
		setup          func(*RedisRepository, *miniredis.Miniredis)
		expectError    bool
		errorContains  string
	}{
		{
			name:           "successful save",
			id:             "test-id",
			circuitBreaker: createTestCircuitBreaker(t),
			expectError:    false,
		},
		{
			name:           "empty ID",
			id:             "",
			circuitBreaker: createTestCircuitBreaker(t),
			expectError:    true,
			errorContains:  "ID cannot be empty",
		},
		{
			name:           "nil circuit breaker",
			id:             "test-id",
			circuitBreaker: nil,
			expectError:    true,
			errorContains:  "circuit breaker cannot be nil",
		},
		{
			name:           "redis connection error",
			id:             "test-id",
			circuitBreaker: createTestCircuitBreaker(t),
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				mr.Close()
			},
			expectError:   true,
			errorContains: "failed to save circuit breaker state to Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			err := repo.Save(tt.id, tt.circuitBreaker)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.True(t, mr.Exists("circuit_breaker:"+tt.id))
			}
		})
	}
}

// FindByID operation tests
func TestRedisRepository_FindByID(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		setup         func(*RedisRepository, *miniredis.Miniredis)
		expectError   bool
		expectNil     bool
		errorContains string
	}{
		{
			name: "successful find",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				cb := createTestCircuitBreaker(&testing.T{})
				repo.Save("test-id", cb)
			},
			expectError: false,
			expectNil:   false,
		},
		{
			name:        "not found",
			id:          "non-existent",
			expectError: false,
			expectNil:   true,
		},
		{
			name:          "empty ID",
			id:            "",
			expectError:   true,
			expectNil:     true,
			errorContains: "ID cannot be empty",
		},
		{
			name: "redis connection error",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				mr.Close()
			},
			expectError:   true,
			expectNil:     true,
			errorContains: "failed to retrieve circuit breaker state from Redis",
		},
		{
			name: "invalid JSON data",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				mr.Set("circuit_breaker:test-id", "invalid-json")
			},
			expectError:   true,
			expectNil:     true,
			errorContains: "failed to unmarshal circuit breaker state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			cb, err := repo.FindByID(tt.id)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}

			if tt.expectNil {
				assert.Nil(t, cb)
			} else {
				assert.NotNil(t, cb)
				assert.Equal(t, core.StateClosed, cb.GetState())
			}
		})
	}
}

// Delete operation tests
func TestRedisRepository_Delete(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		setup         func(*RedisRepository, *miniredis.Miniredis)
		expectError   bool
		errorContains string
	}{
		{
			name: "successful delete",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				cb := createTestCircuitBreaker(&testing.T{})
				repo.Save("test-id", cb)
			},
			expectError: false,
		},
		{
			name:          "not found",
			id:            "non-existent",
			expectError:   true,
			errorContains: "circuit breaker not found",
		},
		{
			name:          "empty ID",
			id:            "",
			expectError:   true,
			errorContains: "ID cannot be empty",
		},
		{
			name: "redis connection error",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				mr.Close()
			},
			expectError:   true,
			errorContains: "failed to delete circuit breaker from Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			err := repo.Delete(tt.id)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.False(t, mr.Exists("circuit_breaker:"+tt.id))
			}
		})
	}
}

// List operation tests
func TestRedisRepository_List(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(*RedisRepository, *miniredis.Miniredis)
		expectedCount int
		expectedIDs   []string
		expectError   bool
		errorContains string
	}{
		{
			name: "list multiple items",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				cb := createTestCircuitBreaker(&testing.T{})
				repo.Save("cb1", cb)
				repo.Save("cb2", cb)
				repo.Save("cb3", cb)
			},
			expectedCount: 3,
			expectedIDs:   []string{"cb1", "cb2", "cb3"},
			expectError:   false,
		},
		{
			name:          "empty list",
			expectedCount: 0,
			expectedIDs:   []string{},
			expectError:   false,
		},
		{
			name: "redis connection error",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				mr.Close()
			},
			expectError:   true,
			errorContains: "failed to list circuit breaker keys from Redis",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			ids, err := repo.List()

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, ids)
			} else {
				assert.NoError(t, err)
				assert.Len(t, ids, tt.expectedCount)
				if len(tt.expectedIDs) > 0 {
					assert.ElementsMatch(t, tt.expectedIDs, ids)
				}
			}
		})
	}
}

// Exists operation tests
func TestRedisRepository_Exists(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		setup         func(*RedisRepository, *miniredis.Miniredis)
		expected      bool
		expectError   bool
		errorContains string
	}{
		{
			name: "exists returns true",
			id:   "test-id",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				cb := createTestCircuitBreaker(&testing.T{})
				repo.Save("test-id", cb)
			},
			expected:    true,
			expectError: false,
		},
		{
			name:        "exists returns false",
			id:          "non-existent",
			expected:    false,
			expectError: false,
		},
		{
			name:          "empty ID",
			id:            "",
			expected:      false,
			expectError:   true,
			errorContains: "ID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			exists, err := repo.Exists(tt.id)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.False(t, exists)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, exists)
			}
		})
	}
}

// Clear operation tests
func TestRedisRepository_Clear(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(*RedisRepository, *miniredis.Miniredis)
		expectError bool
	}{
		{
			name: "clear multiple items",
			setup: func(repo *RedisRepository, mr *miniredis.Miniredis) {
				cb := createTestCircuitBreaker(&testing.T{})
				repo.Save("cb1", cb)
				repo.Save("cb2", cb)
				repo.Save("cb3", cb)
			},
			expectError: false,
		},
		{
			name:        "clear empty repository",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, mr := setupTestRedis(t)

			if tt.setup != nil {
				tt.setup(repo, mr)
			}

			err := repo.Clear()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				// Verify all items are cleared
				ids, listErr := repo.List()
				assert.NoError(t, listErr)
				assert.Empty(t, ids)
			}
		})
	}
}

// Helper method tests
func TestRedisRepository_KeyOperations(t *testing.T) {
	repo, _ := setupTestRedis(t)

	tests := []struct {
		name      string
		operation string
		input     string
		expected  string
	}{
		{
			name:      "build key",
			operation: "build",
			input:     "test-id",
			expected:  "circuit_breaker:test-id",
		},
		{
			name:      "extract ID from valid key",
			operation: "extract",
			input:     "circuit_breaker:test-id",
			expected:  "test-id",
		},
		{
			name:      "extract ID from invalid key",
			operation: "extract",
			input:     "invalid-key",
			expected:  "",
		},
		{
			name:      "extract ID from empty key",
			operation: "extract",
			input:     "",
			expected:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result string
			if tt.operation == "build" {
				result = repo.buildKey(tt.input)
			} else {
				result = repo.extractIDFromKey(tt.input)
			}
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Integration tests
func TestRedisRepository_FullWorkflow(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)
	id := "workflow-test"

	// Complete workflow test
	t.Run("complete workflow", func(t *testing.T) {
		// 1. Initially doesn't exist
		exists, err := repo.Exists(id)
		assert.NoError(t, err)
		assert.False(t, exists)

		// 2. Save circuit breaker
		err = repo.Save(id, cb)
		assert.NoError(t, err)

		// 3. Now exists
		exists, err = repo.Exists(id)
		assert.NoError(t, err)
		assert.True(t, exists)

		// 4. Can find it
		foundCB, err := repo.FindByID(id)
		assert.NoError(t, err)
		assert.NotNil(t, foundCB)

		// 5. Listed in results
		ids, err := repo.List()
		assert.NoError(t, err)
		assert.Contains(t, ids, id)

		// 6. Can delete it
		err = repo.Delete(id)
		assert.NoError(t, err)

		// 7. No longer exists
		exists, err = repo.Exists(id)
		assert.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestRedisRepository_ConcurrentAccess(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	const numGoroutines = 10
	const numOperations = 20

	errChan := make(chan error, numGoroutines*numOperations)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < numOperations; j++ {
				id := fmt.Sprintf("concurrent-%d-%d", goroutineID, j)

				// Save -> Find -> Delete cycle
				if err := repo.Save(id, cb); err != nil {
					errChan <- fmt.Errorf("save failed: %w", err)
					return
				}

				if _, err := repo.FindByID(id); err != nil {
					errChan <- fmt.Errorf("find failed: %w", err)
					return
				}

				if err := repo.Delete(id); err != nil {
					errChan <- fmt.Errorf("delete failed: %w", err)
					return
				}
			}
		}(i)
	}

	// Wait for operations to complete
	time.Sleep(200 * time.Millisecond)

	// Check for errors
	select {
	case err := <-errChan:
		t.Fatalf("concurrent operation failed: %v", err)
	default:
		// No errors, test passed
	}
}

// Benchmark tests
func BenchmarkRedisRepository_Save(b *testing.B) {
	repo, _ := setupTestRedis(&testing.T{})
	cb := createTestCircuitBreaker(&testing.T{})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			repo.Save(fmt.Sprintf("bench-save-%d", i), cb)
			i++
		}
	})
}

func BenchmarkRedisRepository_FindByID(b *testing.B) {
	repo, _ := setupTestRedis(&testing.T{})
	cb := createTestCircuitBreaker(&testing.T{})

	// Pre-populate data
	for i := 0; i < 100; i++ {
		repo.Save(fmt.Sprintf("bench-find-%d", i), cb)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			repo.FindByID(fmt.Sprintf("bench-find-%d", i%100))
			i++
		}
	})
}
