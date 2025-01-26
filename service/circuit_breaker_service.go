package service

import (
	"go-circuit-breaker/core"
	"go-circuit-breaker/ports"
	"time"
)

type CircuitBreakerService struct {
	repo ports.CircuitBreakerRepository
}

func NewCircuitBreakerService(repo ports.CircuitBreakerRepository) *CircuitBreakerService {
	return &CircuitBreakerService{repo: repo}
}

func (s *CircuitBreakerService) Execute(id string, fn func() error) error {
	cb, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if cb == nil {
		cb = core.NewCircuitBreaker(3, 2, 1*time.Second, 5*time.Second)
	}
	err = cb.Execute(fn)
	if err != nil {
		return err
	}
	return s.repo.Save(cb)
}
