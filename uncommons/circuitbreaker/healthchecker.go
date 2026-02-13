package circuitbreaker

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
)

var (
	// ErrNilManager is returned when a nil manager is passed to NewHealthCheckerWithValidation.
	ErrNilManager = errors.New("circuitbreaker: manager must not be nil")
	// ErrInvalidHealthCheckInterval indicates that the health check interval must be positive
	ErrInvalidHealthCheckInterval = errors.New("circuitbreaker: health check interval must be positive")
	// ErrInvalidHealthCheckTimeout indicates that the health check timeout must be positive
	ErrInvalidHealthCheckTimeout = errors.New("circuitbreaker: health check timeout must be positive")
)

// healthChecker performs periodic health checks and manages circuit breaker recovery
type healthChecker struct {
	manager        Manager
	services       map[string]HealthCheckFunc
	interval       time.Duration
	checkTimeout   time.Duration // Timeout for individual health check operations
	logger         log.Logger
	stopChan       chan struct{}
	immediateCheck chan string // Channel to trigger immediate health check for a service
	wg             sync.WaitGroup
	mu             sync.RWMutex
	stopOnce       sync.Once
	started        bool
}

// NewHealthCheckerWithValidation creates a new health checker with validation.
// Returns an error if interval or checkTimeout are not positive.
// interval: how often to run health checks
// checkTimeout: timeout for each individual health check operation
func NewHealthCheckerWithValidation(manager Manager, interval, checkTimeout time.Duration, logger log.Logger) (HealthChecker, error) {
	if manager == nil {
		return nil, ErrNilManager
	}

	if logger == nil {
		return nil, ErrNilLogger
	}

	if interval <= 0 {
		return nil, ErrInvalidHealthCheckInterval
	}

	if checkTimeout <= 0 {
		return nil, ErrInvalidHealthCheckTimeout
	}

	return &healthChecker{
		manager:        manager,
		services:       make(map[string]HealthCheckFunc),
		interval:       interval,
		checkTimeout:   checkTimeout,
		logger:         logger,
		stopChan:       make(chan struct{}),
		immediateCheck: make(chan string, 10),
	}, nil
}

// Register adds a service to health check
func (hc *healthChecker) Register(serviceName string, healthCheckFn HealthCheckFunc) {
	if healthCheckFn == nil {
		hc.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Attempted to register nil health check function for service: %s", serviceName))
		return
	}

	hc.mu.Lock()
	defer hc.mu.Unlock()

	hc.services[serviceName] = healthCheckFn
	hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Registered health check for service: %s", serviceName))
}

// Start begins the health check loop
func (hc *healthChecker) Start() {
	hc.mu.Lock()
	if hc.started {
		hc.mu.Unlock()
		hc.logger.Log(context.Background(), log.LevelWarn, "Health checker already started, ignoring duplicate Start() call")
		return
	}
	hc.started = true
	hc.mu.Unlock()

	hc.wg.Add(1)

	go hc.healthCheckLoop()

	hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Health checker started - checking services every %v", hc.interval))
}

// Stop gracefully stops the health checker
func (hc *healthChecker) Stop() {
	hc.stopOnce.Do(func() {
		close(hc.stopChan)
	})
	hc.wg.Wait()
	hc.logger.Log(context.Background(), log.LevelInfo, "Health checker stopped")
}

func (hc *healthChecker) healthCheckLoop() {
	defer hc.wg.Done()

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	// By entering the select loop immediately, the health checker is responsive
	// to immediate checks from the moment it starts.
	for {
		select {
		case <-ticker.C:
			hc.performHealthChecks()
		case serviceName := <-hc.immediateCheck:
			// Immediate health check for a specific service
			hc.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("Triggering immediate health check for service: %s", serviceName))
			hc.checkServiceHealth(serviceName)
		case <-hc.stopChan:
			return
		}
	}
}

func (hc *healthChecker) performHealthChecks() {
	hc.mu.RLock()
	// Create snapshot to avoid holding lock during checks
	services := make(map[string]HealthCheckFunc, len(hc.services))
	maps.Copy(services, hc.services)

	hc.mu.RUnlock()

	hc.logger.Log(context.Background(), log.LevelDebug, "Performing health checks on registered services...")

	unhealthyCount := 0
	recoveredCount := 0

	for serviceName, healthCheckFn := range services {
		// Skip if circuit breaker is healthy
		if hc.manager.IsHealthy(serviceName) {
			continue
		}

		unhealthyCount++

		hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Attempting to heal service: %s (circuit breaker is open)", serviceName))

		ctx, cancel := context.WithTimeout(context.Background(), hc.checkTimeout)
		err := healthCheckFn(ctx)

		cancel()

		if err == nil {
			hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Service %s recovered - resetting circuit breaker", serviceName))
			hc.manager.Reset(serviceName)

			recoveredCount++
		} else {
			hc.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Service %s still unhealthy: %v - will retry in %v", serviceName, err, hc.interval))
		}
	}

	if unhealthyCount > 0 {
		hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Health check complete: %d services needed healing, %d recovered", unhealthyCount, recoveredCount))
	} else {
		hc.logger.Log(context.Background(), log.LevelDebug, "All services healthy")
	}
}

// GetHealthStatus returns the current health status of all services
func (hc *healthChecker) GetHealthStatus() map[string]string {
	hc.mu.RLock()
	defer hc.mu.RUnlock()

	status := make(map[string]string)

	for serviceName := range hc.services {
		cbState := hc.manager.GetState(serviceName)
		status[serviceName] = string(cbState)
	}

	return status
}

// OnStateChange implements StateChangeListener interface
// This is called when a circuit breaker changes state
func (hc *healthChecker) OnStateChange(serviceName string, from State, to State) {
	hc.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("Health checker notified of state change for %s: %s -> %s", serviceName, from, to))

	// If circuit just opened, trigger immediate health check
	if to == StateOpen {
		hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Circuit breaker opened for %s - scheduling immediate health check", serviceName))

		// Non-blocking send to avoid deadlock
		select {
		case hc.immediateCheck <- serviceName:
			hc.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("Immediate health check scheduled for %s", serviceName))
		default:
			hc.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Immediate health check channel full for %s, will check on next interval", serviceName))
		}
	}
}

// checkServiceHealth performs a health check on a specific service
func (hc *healthChecker) checkServiceHealth(serviceName string) {
	hc.mu.RLock()
	healthCheckFn, exists := hc.services[serviceName]
	hc.mu.RUnlock()

	if !exists {
		hc.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("No health check function registered for service: %s", serviceName))
		return
	}

	// Skip if circuit breaker is already healthy
	if hc.manager.IsHealthy(serviceName) {
		hc.logger.Log(context.Background(), log.LevelDebug, fmt.Sprintf("Service %s is already healthy, skipping check", serviceName))
		return
	}

	hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Attempting to heal service: %s (circuit breaker is open)", serviceName))

	ctx, cancel := context.WithTimeout(context.Background(), hc.checkTimeout)
	err := healthCheckFn(ctx)

	cancel()

	if err == nil {
		hc.logger.Log(context.Background(), log.LevelInfo, fmt.Sprintf("Service %s recovered - resetting circuit breaker", serviceName))
		hc.manager.Reset(serviceName)
	} else {
		hc.logger.Log(context.Background(), log.LevelWarn, fmt.Sprintf("Service %s still unhealthy: %v - will retry in %v", serviceName, err, hc.interval))
	}
}
