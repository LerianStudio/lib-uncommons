package circuitbreaker

import (
	"context"
	"fmt"
	"sync"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/uncommons/runtime"
	"github.com/sony/gobreaker"
)

type manager struct {
	breakers  map[string]*gobreaker.CircuitBreaker
	configs   map[string]Config // Store configs for safe reset
	listeners []StateChangeListener
	mu        sync.RWMutex
	logger    log.Logger
}

// NewManager creates a new circuit breaker manager.
// Returns an error if logger is nil.
func NewManager(logger log.Logger) (Manager, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	return &manager{
		breakers:  make(map[string]*gobreaker.CircuitBreaker),
		configs:   make(map[string]Config),
		listeners: make([]StateChangeListener, 0),
		logger:    logger,
	}, nil
}

// GetOrCreate returns an existing breaker or creates one for the service.
func (m *manager) GetOrCreate(serviceName string, config Config) (CircuitBreaker, error) {
	m.mu.RLock()
	breaker, exists := m.breakers[serviceName]
	m.mu.RUnlock()

	if exists {
		return &circuitBreaker{breaker: breaker}, nil
	}

	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("circuit breaker config for service %s: %w", serviceName, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists = m.breakers[serviceName]; exists {
		return &circuitBreaker{breaker: breaker}, nil
	}

	settings := m.buildSettings(serviceName, config)

	breaker = gobreaker.NewCircuitBreaker(settings)
	m.breakers[serviceName] = breaker
	m.configs[serviceName] = config

	m.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Created circuit breaker for service: %s", serviceName))

	return &circuitBreaker{breaker: breaker}, nil
}

// Execute runs fn through the named service breaker.
func (m *manager) Execute(serviceName string, fn func() (any, error)) (any, error) {
	m.mu.RLock()
	breaker, exists := m.breakers[serviceName]
	m.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("circuit breaker not found for service: %s (call GetOrCreate first)", serviceName)
	}

	result, err := breaker.Execute(fn)
	if err != nil {
		if err == gobreaker.ErrOpenState {
			m.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Circuit breaker [%s] is OPEN - request rejected immediately", serviceName))
			return nil, fmt.Errorf("service %s is currently unavailable (circuit breaker open): %w", serviceName, err)
		}

		if err == gobreaker.ErrTooManyRequests {
			m.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Circuit breaker [%s] is HALF-OPEN - too many test requests", serviceName))
			return nil, fmt.Errorf("service %s is recovering (too many requests): %w", serviceName, err)
		}
	}

	return result, err
}

// GetState returns the current state for a service breaker.
func (m *manager) GetState(serviceName string) State {
	m.mu.RLock()
	breaker, exists := m.breakers[serviceName]
	m.mu.RUnlock()

	if !exists {
		return StateUnknown
	}

	return convertGobreakerState(breaker.State())
}

// GetCounts returns current counters for a service breaker.
func (m *manager) GetCounts(serviceName string) Counts {
	m.mu.RLock()
	breaker, exists := m.breakers[serviceName]
	m.mu.RUnlock()

	if !exists {
		return Counts{}
	}

	counts := breaker.Counts()

	return Counts{
		Requests:             counts.Requests,
		TotalSuccesses:       counts.TotalSuccesses,
		TotalFailures:        counts.TotalFailures,
		ConsecutiveSuccesses: counts.ConsecutiveSuccesses,
		ConsecutiveFailures:  counts.ConsecutiveFailures,
	}
}

// IsHealthy reports whether the service breaker is closed.
func (m *manager) IsHealthy(serviceName string) bool {
	state := m.GetState(serviceName)
	// Only CLOSED state is considered healthy
	// OPEN and HALF-OPEN both need health checker intervention
	isHealthy := state == StateClosed
	m.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("IsHealthy check: service=%s, state=%s, isHealthy=%v", serviceName, state, isHealthy))

	return isHealthy
}

// Reset recreates the service breaker with its stored config.
func (m *manager) Reset(serviceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.breakers[serviceName]; exists {
		m.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Resetting circuit breaker for service: %s", serviceName))

		config, configExists := m.configs[serviceName]
		if !configExists {
			m.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("No stored config found for service %s, cannot recreate", serviceName))
			delete(m.breakers, serviceName)

			return
		}

		settings := m.buildSettings(serviceName, config)

		breaker := gobreaker.NewCircuitBreaker(settings)
		m.breakers[serviceName] = breaker

		m.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Circuit breaker reset completed for service: %s", serviceName))
	}
}

// RegisterStateChangeListener registers a listener for state change notifications
func (m *manager) RegisterStateChangeListener(listener StateChangeListener) {
	if listener == nil {
		m.logger.Log(context.Background(), log.LevelWarn, "Attempted to register a nil state change listener")

		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.listeners = append(m.listeners, listener)
	m.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("Registered state change listener (total: %d)", len(m.listeners)))
}

// handleStateChange processes state changes and notifies listeners
func (m *manager) handleStateChange(serviceName string, from gobreaker.State, to gobreaker.State) {
	// Log state change
	m.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Circuit Breaker [%s] state changed: %s -> %s",
		serviceName, from.String(), to.String()))

	switch to {
	case gobreaker.StateOpen:
		m.logger.Log(context.Background(), log.LevelError, fmt.Sprintf("Circuit Breaker [%s] OPENED - service is unhealthy, requests will fast-fail", serviceName))
	case gobreaker.StateHalfOpen:
		m.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Circuit Breaker [%s] HALF-OPEN - testing service recovery", serviceName))
	case gobreaker.StateClosed:
		m.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Circuit Breaker [%s] CLOSED - service is healthy", serviceName))
	}

	// Notify listeners
	fromState := convertGobreakerState(from)
	toState := convertGobreakerState(to)

	m.mu.RLock()
	listeners := make([]StateChangeListener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.RUnlock()

	for _, listener := range listeners {
		// Notify in goroutine to avoid blocking circuit breaker operations
		listenerCopy := listener

		runtime.SafeGoWithContextAndComponent(
			context.Background(),
			m.logger,
			"circuitbreaker",
			fmt.Sprintf("state_change_listener_%s", serviceName),
			runtime.KeepRunning,
			func(_ context.Context) {
				listenerCopy.OnStateChange(serviceName, fromState, toState)
			},
		)
	}
}

// readyToTrip builds the trip function for gobreaker.Settings.
func readyToTrip(config Config) func(counts gobreaker.Counts) bool {
	return func(counts gobreaker.Counts) bool {
		// Check consecutive failures (skip if threshold is 0 = disabled)
		if config.ConsecutiveFailures > 0 && counts.ConsecutiveFailures >= config.ConsecutiveFailures {
			return true
		}

		// Check failure ratio (skip if min requests is 0 = disabled)
		if config.MinRequests > 0 && counts.Requests >= config.MinRequests {
			failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
			return failureRatio >= config.FailureRatio
		}

		return false
	}
}

// buildSettings creates gobreaker.Settings from a Config for the given service.
func (m *manager) buildSettings(serviceName string, config Config) gobreaker.Settings {
	return gobreaker.Settings{
		Name:        "service-" + serviceName,
		MaxRequests: config.MaxRequests,
		Interval:    config.Interval,
		Timeout:     config.Timeout,
		ReadyToTrip: readyToTrip(config),
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			m.handleStateChange(serviceName, from, to)
		},
	}
}
