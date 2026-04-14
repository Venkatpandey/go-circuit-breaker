//go:build integration

package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/Venkatpandey/go-circuit-breaker/core"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func setupRedisStore(t *testing.T) (*RedisStore, *miniredis.Miniredis) {
	t.Helper()

	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	store, err := NewRedisStore(client, DefaultRedisStoreConfig())
	if err != nil {
		t.Fatalf("new redis store: %v", err)
	}

	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	return store, server
}

func testSnapshot() core.Snapshot {
	return core.Snapshot{
		Config: core.Config{
			FailureThreshold: 3,
			SuccessThreshold: 2,
			CooldownPeriod:   time.Second,
		},
		Stats: core.Stats{
			State:               core.StateOpen,
			ConsecutiveFailures: 3,
			LastFailureTime:     time.Now(),
			LastStateChangeTime: time.Now(),
		},
	}
}

func TestRedisStoreSaveLoadDelete(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()
	snapshot := testSnapshot()

	if err := store.Save(ctx, "payments", snapshot); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := store.Load(ctx, "payments")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected snapshot")
	}
	if loaded.Stats.State != snapshot.Stats.State {
		t.Fatalf("expected state %s, got %s", snapshot.Stats.State, loaded.Stats.State)
	}

	exists, err := store.Exists(ctx, "payments")
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if !exists {
		t.Fatal("expected key to exist")
	}

	if err := store.Delete(ctx, "payments"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	loaded, err = store.Load(ctx, "payments")
	if err != nil {
		t.Fatalf("load after delete: %v", err)
	}
	if loaded != nil {
		t.Fatal("expected snapshot to be deleted")
	}
}

func TestRedisStoreListAndClear(t *testing.T) {
	store, _ := setupRedisStore(t)
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := store.Save(ctx, id, testSnapshot()); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
	}

	ids, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 ids, got %d", len(ids))
	}

	if err := store.Clear(ctx); err != nil {
		t.Fatalf("clear: %v", err)
	}

	ids, err = store.List(ctx)
	if err != nil {
		t.Fatalf("list after clear: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected empty store, got %d ids", len(ids))
	}
}

func TestRedisStorePing(t *testing.T) {
	store, _ := setupRedisStore(t)
	if err := store.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}
