//go:build unit

package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/circuitbreaker"
	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockCBManager implements circuitbreaker.Manager for testing.
type mockCBManager struct {
	state   circuitbreaker.State
	counts  circuitbreaker.Counts
	healthy bool
}

func (m *mockCBManager) GetOrCreate(string, circuitbreaker.Config) (circuitbreaker.CircuitBreaker, error) {
	return nil, nil
}

func (m *mockCBManager) Execute(string, func() (any, error)) (any, error) { return nil, nil }
func (m *mockCBManager) GetState(string) circuitbreaker.State               { return m.state }
func (m *mockCBManager) GetCounts(string) circuitbreaker.Counts             { return m.counts }
func (m *mockCBManager) IsHealthy(string) bool                              { return m.healthy }
func (m *mockCBManager) Reset(string)                                       {}
func (m *mockCBManager) RegisterStateChangeListener(circuitbreaker.StateChangeListener) {
}

func TestHealthWithDependencies_NoDeps(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/health", HealthWithDependencies())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "available", result["status"])
}

func TestHealthWithDependencies_AllHealthy(t *testing.T) {
	t.Parallel()

	mgr := &mockCBManager{
		state:   circuitbreaker.StateClosed,
		counts:  circuitbreaker.Counts{Requests: 10, TotalSuccesses: 10},
		healthy: true,
	}

	app := fiber.New()
	app.Get("/health", HealthWithDependencies(
		DependencyCheck{Name: "database", CircuitBreaker: mgr, ServiceName: "pg"},
	))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var result map[string]any
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "available", result["status"])
}

func TestHealthWithDependencies_MixedHealthy(t *testing.T) {
	t.Parallel()

	healthyMgr := &mockCBManager{state: circuitbreaker.StateClosed, healthy: true}
	unhealthyMgr := &mockCBManager{state: circuitbreaker.StateOpen, healthy: false,
		counts: circuitbreaker.Counts{TotalFailures: 5, ConsecutiveFailures: 3}}

	app := fiber.New()
	app.Get("/health", HealthWithDependencies(
		DependencyCheck{Name: "database", CircuitBreaker: healthyMgr, ServiceName: "pg"},
		DependencyCheck{Name: "cache", CircuitBreaker: unhealthyMgr, ServiceName: "redis"},
	))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	var result map[string]any
	err = json.NewDecoder(resp.Body).Decode(&result)
	require.NoError(t, err)
	assert.Equal(t, "degraded", result["status"])
}

func TestHealthWithDependencies_CustomHealthCheck(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/health", HealthWithDependencies(
		DependencyCheck{
			Name:        "external-api",
			HealthCheck: func() bool { return true },
		},
	))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHealthWithDependencies_CustomHealthCheckUnhealthy(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Get("/health", HealthWithDependencies(
		DependencyCheck{
			Name:        "external-api",
			HealthCheck: func() bool { return false },
		},
	))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestHealthWithDependencies_HealthCheckOverridesCB(t *testing.T) {
	t.Parallel()

	mgr := &mockCBManager{state: circuitbreaker.StateClosed, healthy: true}

	app := fiber.New()
	app.Get("/health", HealthWithDependencies(
		DependencyCheck{
			Name:           "database",
			CircuitBreaker: mgr,
			ServiceName:    "pg",
			HealthCheck:    func() bool { return false },
		},
	))

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
