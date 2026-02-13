//go:build unit

package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHealthCheckerWithValidation_Success(t *testing.T) {
	logger := &log.NopLogger{}
	manager, err := NewManager(logger)
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, 1*time.Second, 500*time.Millisecond, logger)

	assert.NoError(t, err)
	assert.NotNil(t, hc)
}

func TestNewHealthCheckerWithValidation_InvalidInterval(t *testing.T) {
	logger := &log.NopLogger{}
	manager, err := NewManager(logger)
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, 0, 500*time.Millisecond, logger)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidHealthCheckInterval))
}

func TestNewHealthCheckerWithValidation_NegativeInterval(t *testing.T) {
	logger := &log.NopLogger{}
	manager, err := NewManager(logger)
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, -1*time.Second, 500*time.Millisecond, logger)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidHealthCheckInterval))
}

func TestNewHealthCheckerWithValidation_InvalidTimeout(t *testing.T) {
	logger := &log.NopLogger{}
	manager, err := NewManager(logger)
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, 1*time.Second, 0, logger)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidHealthCheckTimeout))
}

func TestNewHealthCheckerWithValidation_NegativeTimeout(t *testing.T) {
	logger := &log.NopLogger{}
	manager, err := NewManager(logger)
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, 1*time.Second, -500*time.Millisecond, logger)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrInvalidHealthCheckTimeout))
}

func TestNewHealthCheckerWithValidation_NilManager(t *testing.T) {
	logger := &log.NopLogger{}

	hc, err := NewHealthCheckerWithValidation(nil, 1*time.Second, 500*time.Millisecond, logger)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNilManager))
}

func TestNewHealthCheckerWithValidation_NilLogger(t *testing.T) {
	manager, err := NewManager(&log.NopLogger{})
	require.NoError(t, err)

	hc, err := NewHealthCheckerWithValidation(manager, 1*time.Second, 500*time.Millisecond, nil)

	assert.Nil(t, hc)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNilLogger))
}
