package core

import (
	"errors"
	"time"
)

var (
	ErrCircuitOpen  = errors.New("circuit breaker is open")
	ErrMaxAttempts  = errors.New("max attempts reached")
	ErrInvalidState = errors.New("invalid state")
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

type CircuitBreaker struct {
	State            State         `json:"state"`
	FailureThreshold int           `json:"failure_threshold"`
	SuccessThreshold int           `json:"success_threshold"`
	FailureCount     int           `json:"failure_count"`
	SuccessCount     int           `json:"success_count"`
	Timeout          time.Duration `json:"timeout"`
	LastFailureTime  time.Time     `json:"last_failure_time"`
	CooldownPeriod   time.Duration `json:"cooldown_period"`
}

func NewCircuitBreaker(failureThreshold, successThreshold int, timeout, cooldownPeriod time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		State:            StateClosed,
		FailureThreshold: failureThreshold,
		SuccessThreshold: successThreshold,
		Timeout:          timeout,
		CooldownPeriod:   cooldownPeriod,
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	if cb.State == StateOpen {
		if time.Since(cb.LastFailureTime) < cb.CooldownPeriod {
			return ErrCircuitOpen
		}
		cb.State = StateHalfOpen
	}

	if cb.State == StateHalfOpen {
		if cb.SuccessCount >= cb.SuccessThreshold {
			cb.State = StateClosed
			cb.SuccessCount = 0
		} else if cb.FailureCount >= cb.FailureThreshold {
			cb.State = StateOpen
			cb.LastFailureTime = time.Now()
			cb.FailureCount = 0
			return ErrCircuitOpen
		}
	}

	err := fn()
	if err != nil {
		cb.FailureCount++
		if cb.FailureCount >= cb.FailureThreshold {
			cb.State = StateOpen
			cb.LastFailureTime = time.Now()
		}
		return err
	}

	cb.SuccessCount++
	if cb.State == StateHalfOpen && cb.SuccessCount >= cb.SuccessThreshold {
		cb.State = StateClosed
		cb.SuccessCount = 0
	}

	return nil
}
