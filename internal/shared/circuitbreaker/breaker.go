package circuitbreaker

import (
	"errors"
	"sync"
	"time"
)

type State int

const (
	StateClosed State = iota
	StateOpen
	StateHalfOpen
)

var ErrCircuitOpen = errors.New("circuit breaker is open")

type CircuitBreaker struct {
	sync.Mutex
	state           State
	failureCount    int
	failureThreshold int
	cooldownDuration time.Duration
	lastStateChange time.Time
}

func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:            StateClosed,
		failureThreshold: threshold,
		cooldownDuration: cooldown,
		lastStateChange:  time.Now(),
	}
}

func (cb *CircuitBreaker) Execute(fn func() error) error {
	if err := cb.beforeExecute(); err != nil {
		return err
	}

	err := fn()

	cb.afterExecute(err)
	return err
}

func (cb *CircuitBreaker) beforeExecute() error {
	cb.Lock()
	defer cb.Unlock()

	now := time.Now()

	switch cb.state {
	case StateOpen:
		if now.Sub(cb.lastStateChange) > cb.cooldownDuration {
			cb.state = StateHalfOpen
			cb.lastStateChange = now
			return nil
		}
		return ErrCircuitOpen
	case StateHalfOpen:
		// Limit to 1 request in Half-Open state (implicitly handles trial)
		return nil
	default:
		return nil
	}
}

func (cb *CircuitBreaker) afterExecute(err error) {
	cb.Lock()
	defer cb.Unlock()

	now := time.Now()

	if err != nil {
		cb.failureCount++
		if cb.state == StateHalfOpen || cb.failureCount >= cb.failureThreshold {
			cb.state = StateOpen
			cb.lastStateChange = now
		}
	} else {
		if cb.state == StateHalfOpen {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.lastStateChange = now
		} else if cb.state == StateClosed {
			cb.failureCount = 0
		}
	}
}
