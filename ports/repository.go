package ports

import (
	"context"

	"github.com/Venkatpandey/go-circuit-breaker/core"
)

// SnapshotStore persists and lists breaker snapshots by breaker ID.
type SnapshotStore interface {
	// Load returns nil,nil when no snapshot exists for id.
	Load(ctx context.Context, id string) (*core.Snapshot, error)
	// Save writes snapshot for id.
	Save(ctx context.Context, id string, snapshot core.Snapshot) error
	// Delete removes snapshot for id.
	Delete(ctx context.Context, id string) error
	// List returns all known breaker IDs.
	List(ctx context.Context) ([]string, error)
}
