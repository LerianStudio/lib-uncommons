package zap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestNewRejectsMissingOTelLibraryName(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Environment: EnvironmentProduction})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OTelLibraryName is required")
}

func TestNewRejectsInvalidEnvironment(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Environment: Environment("banana"), OTelLibraryName: "svc"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid environment")
}

func TestNewAppliesEnvironmentDefaultLevel(t *testing.T) {
	t.Parallel()

	logger, err := New(Config{Environment: EnvironmentDevelopment, OTelLibraryName: "svc"})
	require.NoError(t, err)
	assert.Equal(t, zapcore.DebugLevel, logger.Level().Level())

	logger, err = New(Config{Environment: EnvironmentProduction, OTelLibraryName: "svc"})
	require.NoError(t, err)
	assert.Equal(t, zapcore.InfoLevel, logger.Level().Level())
}

func TestNewAppliesCustomLevel(t *testing.T) {
	t.Parallel()

	logger, err := New(Config{Environment: EnvironmentProduction, OTelLibraryName: "svc", Level: "error"})
	require.NoError(t, err)
	assert.Equal(t, zapcore.ErrorLevel, logger.Level().Level())
}

func TestNewRejectsInvalidCustomLevel(t *testing.T) {
	t.Parallel()

	_, err := New(Config{Environment: EnvironmentProduction, OTelLibraryName: "svc", Level: "invalid"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid level")
}

func TestCallerSkipFramesConstant(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, callerSkipFrames)
}
