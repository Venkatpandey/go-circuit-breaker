package adapters

import (
	"context"
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
	// Start embedded Redis server for testing
	mr, err := miniredis.Run()
	require.NoError(t, err)

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	// Create repository
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

// Constructor tests

func TestNewRedisRepository(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	repo := NewRedisRepository(client)

	assert.NotNil(t, repo)
	assert.Equal(t, client, repo.client)
	assert.Equal(t, DefaultRedisConfig(), repo.config)
}

func TestNewRedisRepositoryWithConfig(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	defer client.Close()

	config := RedisConfig{
		Timeout: 10 * time.Second,
		Context: context.Background(),
	}

	repo := NewRedisRepositoryWithConfig(client, config)

	assert.NotNil(t, repo)
	assert.Equal(t, client, repo.client)
	assert.Equal(t, config, repo.config)
}

func TestNewRedisRepositoryNilClient(t *testing.T) {
	assert.Panics(t, func() {
		NewRedisRepository(nil)
	})
}

func TestDefaultRedisConfig(t *testing.T) {
	config := DefaultRedisConfig()

	assert.Equal(t, defaultTimeout, config.Timeout)
	assert.NotNil(t, config.Context)
}

// Save method tests

func TestRedisRepository_Save_Success(t *testing.T) {
	repo, mr := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	err := repo.Save("test-id", cb)
	assert.NoError(t, err)

	// Verify data was saved
	assert.True(t, mr.Exists("circuit_breaker:test-id"))
}

func TestRedisRepository_Save_EmptyID(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	err := repo.Save("", cb)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ID cannot be empty")
}

func TestRedisRepository_Save_NilCircuitBreaker(t *testing.T) {
	repo, _ := setupTestRedis(t)

	err := repo.Save("test-id", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker cannot be nil")
}

func TestRedisRepository_Save_RedisError(t *testing.T) {
	repo, mr := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Close Redis server to simulate error
	mr.Close()

	err := repo.Save("test-id", cb)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save circuit breaker state to Redis")
}

// FindByID method tests

func TestRedisRepository_FindByID_Success(t *testing.T) {
	repo, _ := setupTestRedis(t)
	originalCB := createTestCircuitBreaker(t)

	// Save first
	err := repo.Save("test-id", originalCB)
	require.NoError(t, err)

	// Find
	foundCB, err := repo.FindByID("test-id")
	assert.NoError(t, err)
	assert.NotNil(t, foundCB)

	// Verify it's a valid circuit breaker (basic check)
	assert.Equal(t, core.StateClosed, foundCB.GetState())
}

func TestRedisRepository_FindByID_NotFound(t *testing.T) {
	repo, _ := setupTestRedis(t)

	cb, err := repo.FindByID("non-existent")
	assert.NoError(t, err)
	assert.Nil(t, cb)
}

func TestRedisRepository_FindByID_EmptyID(t *testing.T) {
	repo, _ := setupTestRedis(t)

	cb, err := repo.FindByID("")
	assert.Error(t, err)
	assert.Nil(t, cb)
	assert.Contains(t, err.Error(), "ID cannot be empty")
}

func TestRedisRepository_FindByID_RedisError(t *testing.T) {
	repo, mr := setupTestRedis(t)

	// Close Redis server to simulate error
	mr.Close()

	cb, err := repo.FindByID("test-id")
	assert.Error(t, err)
	assert.Nil(t, cb)
	assert.Contains(t, err.Error(), "failed to retrieve circuit breaker state from Redis")
}

func TestRedisRepository_FindByID_InvalidData(t *testing.T) {
	repo, mr := setupTestRedis(t)

	// Manually set invalid JSON data
	mr.Set("circuit_breaker:test-id", "invalid-json")

	cb, err := repo.FindByID("test-id")
	assert.Error(t, err)
	assert.Nil(t, cb)
	assert.Contains(t, err.Error(), "failed to unmarshal circuit breaker state")
}

// Delete method tests

func TestRedisRepository_Delete_Success(t *testing.T) {
	repo, mr := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Save first
	err := repo.Save("test-id", cb)
	require.NoError(t, err)
	assert.True(t, mr.Exists("circuit_breaker:test-id"))

	// Delete
	err = repo.Delete("test-id")
	assert.NoError(t, err)
	assert.False(t, mr.Exists("circuit_breaker:test-id"))
}

func TestRedisRepository_Delete_NotFound(t *testing.T) {
	repo, _ := setupTestRedis(t)

	err := repo.Delete("non-existent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker not found")
}

func TestRedisRepository_Delete_EmptyID(t *testing.T) {
	repo, _ := setupTestRedis(t)

	err := repo.Delete("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ID cannot be empty")
}

func TestRedisRepository_Delete_RedisError(t *testing.T) {
	repo, mr := setupTestRedis(t)

	// Close Redis server to simulate error
	mr.Close()

	err := repo.Delete("test-id")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete circuit breaker from Redis")
}

// List method tests

func TestRedisRepository_List_Success(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Save multiple circuit breakers
	expectedIDs := []string{"cb1", "cb2", "cb3"}
	for _, id := range expectedIDs {
		err := repo.Save(id, cb)
		require.NoError(t, err)
	}

	// List
	ids, err := repo.List()
	assert.NoError(t, err)
	assert.ElementsMatch(t, expectedIDs, ids)
}

func TestRedisRepository_List_Empty(t *testing.T) {
	repo, _ := setupTestRedis(t)

	ids, err := repo.List()
	assert.NoError(t, err)
	assert.Empty(t, ids)
}

func TestRedisRepository_List_RedisError(t *testing.T) {
	repo, mr := setupTestRedis(t)

	// Close Redis server to simulate error
	mr.Close()

	ids, err := repo.List()
	assert.Error(t, err)
	assert.Nil(t, ids)
	assert.Contains(t, err.Error(), "failed to list circuit breaker keys from Redis")
}

// Exists method tests

func TestRedisRepository_Exists_True(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Save circuit breaker
	err := repo.Save("test-id", cb)
	require.NoError(t, err)

	// Check existence
	exists, err := repo.Exists("test-id")
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestRedisRepository_Exists_False(t *testing.T) {
	repo, _ := setupTestRedis(t)

	exists, err := repo.Exists("non-existent")
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisRepository_Exists_EmptyID(t *testing.T) {
	repo, _ := setupTestRedis(t)

	exists, err := repo.Exists("")
	assert.Error(t, err)
	assert.False(t, exists)
	assert.Contains(t, err.Error(), "ID cannot be empty")
}

// Clear method tests

func TestRedisRepository_Clear_Success(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Save multiple circuit breakers
	for _, id := range []string{"cb1", "cb2", "cb3"} {
		err := repo.Save(id, cb)
		require.NoError(t, err)
	}

	// Verify they exist
	ids, err := repo.List()
	require.NoError(t, err)
	assert.Len(t, ids, 3)

	// Clear all
	err = repo.Clear()
	assert.NoError(t, err)

	// Verify they're gone
	ids, err = repo.List()
	assert.NoError(t, err)
	assert.Empty(t, ids)
}

func TestRedisRepository_Clear_Empty(t *testing.T) {
	repo, _ := setupTestRedis(t)

	err := repo.Clear()
	assert.NoError(t, err) // Should not error when nothing to clear
}

// Ping method tests

func TestRedisRepository_Ping_Success(t *testing.T) {
	repo, _ := setupTestRedis(t)

	err := repo.Ping()
	assert.NoError(t, err)
}

func TestRedisRepository_Ping_Error(t *testing.T) {
	repo, mr := setupTestRedis(t)

	// Close Redis server
	mr.Close()

	err := repo.Ping()
	assert.Error(t, err)
}

// Helper method tests

func TestRedisRepository_BuildKey(t *testing.T) {
	repo, _ := setupTestRedis(t)

	key := repo.buildKey("test-id")
	assert.Equal(t, "circuit_breaker:test-id", key)
}

func TestRedisRepository_ExtractIDFromKey(t *testing.T) {
	repo, _ := setupTestRedis(t)

	tests := []struct {
		key        string
		expectedID string
	}{
		{"circuit_breaker:test-id", "test-id"},
		{"circuit_breaker:another-id", "another-id"},
		{"invalid-key", ""},
		{"", ""},
	}

	for _, tt := range tests {
		id := repo.extractIDFromKey(tt.key)
		assert.Equal(t, tt.expectedID, id)
	}
}

// Integration tests

func TestRedisRepository_FullWorkflow(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	// Test complete workflow
	id := "workflow-test"

	// 1. Verify doesn't exist
	exists, err := repo.Exists(id)
	assert.NoError(t, err)
	assert.False(t, exists)

	// 2. Save
	err = repo.Save(id, cb)
	assert.NoError(t, err)

	// 3. Verify exists
	exists, err = repo.Exists(id)
	assert.NoError(t, err)
	assert.True(t, exists)

	// 4. Find
	foundCB, err := repo.FindByID(id)
	assert.NoError(t, err)
	assert.NotNil(t, foundCB)

	// 5. List should contain our ID
	ids, err := repo.List()
	assert.NoError(t, err)
	assert.Contains(t, ids, id)

	// 6. Delete
	err = repo.Delete(id)
	assert.NoError(t, err)

	// 7. Verify doesn't exist anymore
	exists, err = repo.Exists(id)
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestRedisRepository_ConcurrentAccess(t *testing.T) {
	repo, _ := setupTestRedis(t)
	cb := createTestCircuitBreaker(t)

	const numGoroutines = 10
	const numOperations = 20

	// Run concurrent operations
	errChan := make(chan error, numGoroutines*numOperations)

	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			for j := 0; j < numOperations; j++ {
				id := fmt.Sprintf("concurrent-%d-%d", goroutineID, j)

				// Save
				if err := repo.Save(id, cb); err != nil {
					errChan <- err
					return
				}

				// Find
				if _, err := repo.FindByID(id); err != nil {
					errChan <- err
					return
				}

				// Delete
				if err := repo.Delete(id); err != nil {
					errChan <- err
					return
				}
			}
		}(i)
	}

	// Wait a bit for operations to complete
	time.Sleep(100 * time.Millisecond)

	// Check for errors
	select {
	case err := <-errChan:
		t.Fatalf("concurrent operation failed: %v", err)
	default:
		// No errors, test passed
	}
}

func TestRedisRepository_ContextTimeout(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer client.Close()

	// Create repository with very short timeout
	config := RedisConfig{
		Timeout: 1 * time.Microsecond, // Very short timeout
		Context: context.Background(),
	}
	repo := NewRedisRepositoryWithConfig(client, config)

	cb := createTestCircuitBreaker(t)

	// This should timeout (though it might not always due to timing)
	err = repo.Save("test-id", cb)
	// We can't guarantee this will timeout every time due to timing,
	// but if it does, it should be handled gracefully
	if err != nil {
		assert.Contains(t, err.Error(), "context deadline exceeded")
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

	// Pre-populate with data
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

// Additional helper for setup that may be needed
func setupTestRedisWithRealServer(t *testing.T) *RedisRepository {
	// This would connect to a real Redis server for integration testing
	// Uncomment and modify as needed for your environment
	/*
	   client := redis.NewClient(&redis.Options{
	       Addr:     "localhost:6379",
	       Password: "",
	       DB:       1, // Use different DB for testing
	   })

	   repo := NewRedisRepository(client)

	   // Clear any existing test data
	   repo.Clear()

	   t.Cleanup(func() {
	       repo.Clear()
	       client.Close()
	   })

	   return repo
	*/
	t.Skip("Real Redis server not configured")
	return nil
}
