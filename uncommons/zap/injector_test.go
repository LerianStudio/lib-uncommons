package zap

// Note on error path testing for InitializeLoggerWithError:
// The zap logger Build() function only returns an error in cases that are
// difficult to simulate in unit tests (e.g., invalid output paths, encoder errors).
// With the default configuration used in InitializeLoggerWithError, the Build()
// call is very unlikely to fail.
//
// The error path IS covered in InitializeLoggerWithError (injector.go):
// When zap.Build() fails, the function returns a wrapped error via
// fmt.Errorf("can't initialize zap logger: %w", err)
// This ensures proper error chaining for callers using errors.Is() or errors.As().
//
// To trigger an actual error in Build(), one would need to:
//   - Provide an invalid output path (not possible with current implementation)
//   - Corrupt the zap configuration (not exposed)
//
// Therefore, error handling exists and is correct, but cannot be easily tested
// without modifying the production code to accept external configuration.

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- InitializeLoggerFromConfig tests (injectable, no env deps) ---

func TestInitializeLoggerFromConfig_Production(t *testing.T) {
	cfg := LoggerConfig{
		EnvName:         "production",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)

	typed, ok := logger.(*ZapWithTraceLogger)
	require.True(t, ok)
	assert.NotNil(t, typed.Logger)
}

func TestInitializeLoggerFromConfig_Development(t *testing.T) {
	cfg := LoggerConfig{
		EnvName:         "development",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
}

func TestInitializeLoggerFromConfig_CaseInsensitiveEnvName(t *testing.T) {
	tests := []struct {
		name    string
		envName string
	}{
		{"uppercase PRODUCTION", "PRODUCTION"},
		{"mixed case Production", "Production"},
		{"staging", "staging"},
		{"Staging", "Staging"},
		{"UAT", "UAT"},
		{"Local", "Local"},
		{"DEVELOPMENT", "DEVELOPMENT"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := LoggerConfig{
				EnvName:         tc.envName,
				OtelLibraryName: "test-lib",
			}

			logger, err := InitializeLoggerFromConfig(cfg)
			require.NoError(t, err)
			require.NotNil(t, logger)
		})
	}
}

func TestInitializeLoggerFromConfig_DefaultFallback(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)

	defer log.SetOutput(originalOutput)

	cfg := LoggerConfig{
		EnvName:         "",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.Contains(t, buf.String(), "WARNING")
	assert.Contains(t, buf.String(), "defaulting to production config")
}

func TestInitializeLoggerFromConfig_UnrecognizedEnvName(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)

	defer log.SetOutput(originalOutput)

	cfg := LoggerConfig{
		EnvName:         "banana",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.Contains(t, buf.String(), "WARNING")
	assert.Contains(t, buf.String(), "banana")
}

func TestInitializeLoggerFromConfig_CustomLogLevel(t *testing.T) {
	cfg := LoggerConfig{
		EnvName:         "production",
		LogLevel:        "warn",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)

	typed, ok := logger.(*ZapWithTraceLogger)
	require.True(t, ok)
	assert.NotNil(t, typed.Logger)
}

func TestInitializeLoggerFromConfig_InvalidLogLevel(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)

	defer log.SetOutput(originalOutput)

	cfg := LoggerConfig{
		EnvName:         "production",
		LogLevel:        "invalid_level",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.Contains(t, buf.String(), "Invalid LOG_LEVEL")
	assert.Contains(t, buf.String(), "fallback to InfoLevel")
}

func TestInitializeLoggerFromConfig_EmptyOtelLibraryName(t *testing.T) {
	var buf bytes.Buffer
	originalOutput := log.Writer()
	log.SetOutput(&buf)

	defer log.SetOutput(originalOutput)

	cfg := LoggerConfig{
		EnvName:         "production",
		OtelLibraryName: "",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
	assert.Contains(t, buf.String(), "OTEL_LIBRARY_NAME not set")
	assert.Contains(t, buf.String(), "unknown")
}

func TestInitializeLoggerFromConfig_NoLogLevelOverride(t *testing.T) {
	// When LogLevel is empty, no override should be applied — uses the env default.
	cfg := LoggerConfig{
		EnvName:         "development",
		LogLevel:        "",
		OtelLibraryName: "test-lib",
	}

	logger, err := InitializeLoggerFromConfig(cfg)

	require.NoError(t, err)
	require.NotNil(t, logger)
}

// --- ConfigFromEnv tests ---

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("ENV_NAME", "staging")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("OTEL_LIBRARY_NAME", "my-service")

	cfg := ConfigFromEnv()

	assert.Equal(t, "staging", cfg.EnvName)
	assert.Equal(t, "error", cfg.LogLevel)
	assert.Equal(t, "my-service", cfg.OtelLibraryName)
}

func TestConfigFromEnv_Empty(t *testing.T) {
	t.Setenv("ENV_NAME", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("OTEL_LIBRARY_NAME", "")

	cfg := ConfigFromEnv()

	assert.Empty(t, cfg.EnvName)
	assert.Empty(t, cfg.LogLevel)
	assert.Empty(t, cfg.OtelLibraryName)
}

// --- Legacy env-based wrappers (backward compat) ---

func TestInitializeLogger(t *testing.T) {
	t.Setenv("ENV_NAME", "production")

	logger := InitializeLogger()
	assert.NotNil(t, logger)
}

func TestInitializeLoggerWithError_Success(t *testing.T) {
	t.Setenv("ENV_NAME", "production")

	logger, err := InitializeLoggerWithError()

	require.NoError(t, err)
	require.NotNil(t, logger)

	typed, ok := logger.(*ZapWithTraceLogger)
	require.True(t, ok)
	assert.NotNil(t, typed.Logger)
}

func TestCallerSkipFramesConstant(t *testing.T) {
	t.Parallel()

	// Verify the constant exists and has the expected value.
	// If the hydration layer structure changes, this test will remind
	// the developer to re-evaluate the caller skip value.
	assert.Equal(t, 2, callerSkipFrames)
}
