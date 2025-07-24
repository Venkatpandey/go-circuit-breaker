package ports

import "go-circuit-breaker/core"

type CircuitBreakerRepository interface {
	FindByID(id string) (*core.CircuitBreaker, error)
	Save(id string, cb *core.CircuitBreaker) error
	Delete(id string) error
	List() ([]string, error)
}
