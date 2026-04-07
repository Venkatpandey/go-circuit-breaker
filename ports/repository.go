package ports

import (
	"context"

	"go-circuit-breaker/core"
)

type SnapshotStore interface {
	Load(ctx context.Context, id string) (*core.Snapshot, error)
	Save(ctx context.Context, id string, snapshot core.Snapshot) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]string, error)
}
