package adapters

import (
	"go-circuit-breaker/core"
	"sync"
)

type MemoryRepository struct {
	store map[string]*core.CircuitBreaker
	mu    sync.Mutex
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		store: make(map[string]*core.CircuitBreaker),
	}
}

func (r *MemoryRepository) Save(cb *core.CircuitBreaker) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.store["default"] = cb
	return nil
}

func (r *MemoryRepository) FindByID(id string) (*core.CircuitBreaker, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cb, exists := r.store[id]; exists {
		return cb, nil
	}
	return nil, nil
}
