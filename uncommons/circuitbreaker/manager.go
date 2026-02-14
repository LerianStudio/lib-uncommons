package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	constant "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry/metrics"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/runtime"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/safe"
	"github.com/sony/gobreaker"
)

// stateChangeListenerTimeout limits how long a state change listener notification
// can run before the context is cancelled.
const stateChangeListenerTimeout = 10 * time.Second

type manager struct {
	breakers       map[string]*gobreaker.CircuitBreaker
	configs        map[string]Config // Store configs for safe reset
	listeners      []StateChangeListener
	mu             sync.RWMutex
	logger         log.Logger
	metricsFactory *metrics.MetricsFactory
	stateCounter   *metrics.CounterBuilder
	execCounter    *metrics.CounterBuilder
}

// ManagerOption configures optional behaviour on a circuit breaker manager.
type ManagerOption func(*manager)

// WithMetricsFactory attaches a MetricsFactory so the manager emits
// circuit_breaker_state_transitions_total and circuit_breaker_executions_total
// counters automatically.  When nil, metrics are silently skipped.
func WithMetricsFactory(f *metrics.MetricsFactory) ManagerOption {
	return func(m *manager) {
		m.metricsFactory = f
	}
}

// stateTransitionMetric defines the counter for circuit breaker state transitions.
var stateTransitionMetric = metrics.Metric{
	Name:        "circuit_breaker_state_transitions_total",
	Unit:        "1",
	Description: "Total number of circuit breaker state transitions",
}

// executionMetric defines the counter for circuit breaker executions.
var executionMetric = metrics.Metric{
	Name:        "circuit_breaker_executions_total",
	Unit:        "1",
	Description: "Total number of circuit breaker executions",
}

// NewManager creates a new circuit breaker manager.
// Returns an error if logger is nil.
func NewManager(logger log.Logger, opts ...ManagerOption) (Manager, error) {
	if logger == nil {
		return nil, ErrNilLogger
	}

	m := &manager{
		breakers:  make(map[string]*gobreaker.CircuitBreaker),
		configs:   make(map[string]Config),
		listeners: make([]StateChangeListener, 0),
		logger:    logger,
	}

	for _, opt := range opts {
		opt(m)
	}

	m.initMetricCounters()

	return m, nil
}

func (m *manager) initMetricCounters() {
	if m.metricsFactory == nil {
		return
	}

	stateCounter, err := m.metricsFactory.Counter(stateTransitionMetric)
	if err != nil {
		m.logger.Log(context.Background(), log.LevelWarn, "failed to create state transition metric counter", log.Err(err))
	} else {
		m.stateCounter = stateCounter
	}

	execCounter, err := m.metricsFactory.Counter(executionMetric)
	if err != nil {
		m.logger.Log(context.Background(), log.LevelWarn, "failed to create execution metric counter", log.Err(err))
	} else {
		m.execCounter = execCounter
	}
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

	m.logger.Log(context.Background(), log.LevelInfo, "created circuit breaker", log.String("service", serviceName))

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
		if errors.Is(err, gobreaker.ErrOpenState) {
			m.logger.Log(context.Background(), log.LevelWarn, "circuit breaker is OPEN, request rejected", log.String("service", serviceName))
			m.recordExecution(serviceName, "rejected_open")

			return nil, fmt.Errorf("service %s is currently unavailable (circuit breaker open): %w", serviceName, err)
		}

		if errors.Is(err, gobreaker.ErrTooManyRequests) {
			m.logger.Log(context.Background(), log.LevelWarn, "circuit breaker is HALF-OPEN, too many test requests", log.String("service", serviceName))
			m.recordExecution(serviceName, "rejected_half_open")

			return nil, fmt.Errorf("service %s is recovering (too many requests): %w", serviceName, err)
		}

		// The wrapped function returned an error (not a breaker rejection)
		m.recordExecution(serviceName, "error")

		return result, err
	}

	m.recordExecution(serviceName, "success")

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
	m.logger.Log(context.Background(), log.LevelDebug, "health check result", log.String("service", serviceName), log.String("state", string(state)), log.Bool("healthy", isHealthy))

	return isHealthy
}

// Reset recreates the service breaker with its stored config.
func (m *manager) Reset(serviceName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.breakers[serviceName]; exists {
		m.logger.Log(context.Background(), log.LevelInfo, "resetting circuit breaker", log.String("service", serviceName))

		config, configExists := m.configs[serviceName]
		if !configExists {
			m.logger.Log(context.Background(), log.LevelWarn, "no stored config found, cannot recreate circuit breaker", log.String("service", serviceName))
			delete(m.breakers, serviceName)

			return
		}

		settings := m.buildSettings(serviceName, config)

		breaker := gobreaker.NewCircuitBreaker(settings)
		m.breakers[serviceName] = breaker

		m.logger.Log(context.Background(), log.LevelInfo, "circuit breaker reset completed", log.String("service", serviceName))
	}
}

// RegisterStateChangeListener registers a listener for state change notifications
func (m *manager) RegisterStateChangeListener(listener StateChangeListener) {
	if listener == nil {
		m.logger.Log(context.Background(), log.LevelWarn, "attempted to register a nil state change listener")

		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.listeners = append(m.listeners, listener)
	m.logger.Log(context.Background(), log.LevelDebug, "registered state change listener", log.Int("total", len(m.listeners)))
}

// handleStateChange processes state changes and notifies listeners
func (m *manager) handleStateChange(serviceName string, from gobreaker.State, to gobreaker.State) {
	// Log state change
	m.logger.Log(context.Background(), log.LevelWarn, "circuit breaker state changed", log.String("service", serviceName), log.String("from", from.String()), log.String("to", to.String()))

	switch to {
	case gobreaker.StateOpen:
		m.logger.Log(context.Background(), log.LevelError, "circuit breaker OPENED, requests will fast-fail", log.String("service", serviceName))
	case gobreaker.StateHalfOpen:
		m.logger.Log(context.Background(), log.LevelInfo, "circuit breaker HALF-OPEN, testing service recovery", log.String("service", serviceName))
	case gobreaker.StateClosed:
		m.logger.Log(context.Background(), log.LevelInfo, "circuit breaker CLOSED, service is healthy", log.String("service", serviceName))
	}

	// Record state transition metric
	fromState := convertGobreakerState(from)
	toState := convertGobreakerState(to)

	m.recordStateTransition(serviceName, fromState, toState)

	m.mu.RLock()
	listeners := make([]StateChangeListener, len(m.listeners))
	copy(listeners, m.listeners)
	m.mu.RUnlock()

	for _, listener := range listeners {
		// Notify in goroutine to avoid blocking circuit breaker operations.
		// A timeout context prevents slow or stuck listeners from leaking goroutines.
		listenerCopy := listener
		listenerCtx, listenerCancel := context.WithTimeout(context.Background(), stateChangeListenerTimeout)

		runtime.SafeGoWithContextAndComponent(
			listenerCtx,
			m.logger,
			"circuitbreaker",
			"state_change_listener_"+serviceName,
			runtime.KeepRunning,
			func(_ context.Context) {
				defer listenerCancel()

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
			failureRatio := safe.DivideFloat64OrZero(float64(counts.TotalFailures), float64(counts.Requests))
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

// recordStateTransition increments the state transition counter.
// No-op when metricsFactory is nil.
func (m *manager) recordStateTransition(serviceName string, from, to State) {
	if m.stateCounter == nil {
		return
	}

	err := m.stateCounter.
		WithLabels(map[string]string{
			"service":    constant.SanitizeMetricLabel(serviceName),
			"from_state": string(from),
			"to_state":   string(to),
		}).
		AddOne(context.Background())
	if err != nil {
		m.logger.Log(context.Background(), log.LevelWarn, "failed to record state transition metric", log.Err(err))
	}
}

// recordExecution increments the execution counter.
// No-op when metricsFactory is nil.
func (m *manager) recordExecution(serviceName, result string) {
	if m.execCounter == nil {
		return
	}

	err := m.execCounter.
		WithLabels(map[string]string{
			"service": constant.SanitizeMetricLabel(serviceName),
			"result":  result,
		}).
		AddOne(context.Background())
	if err != nil {
		m.logger.Log(context.Background(), log.LevelWarn, "failed to record execution metric", log.Err(err))
	}
}
