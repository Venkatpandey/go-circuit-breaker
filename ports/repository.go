package ports

import "go-circuit-breaker/core"

type CircuitBreakerRepository interface {
	Save(cb *core.CircuitBreaker) error
	FindByID(id string) (*core.CircuitBreaker, error)
}
