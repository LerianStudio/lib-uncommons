package zap

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func newObservedLogger(level zapcore.Level) (*Logger, *observer.ObservedLogs) {
	core, observed := observer.New(level)

	return &Logger{logger: zap.New(core)}, observed
}

// newBufferedLogger creates a Logger that writes JSON to a buffer for output
// inspection (e.g., verifying CWE-117 sanitization in serialized output).
func newBufferedLogger(level zapcore.Level) (*Logger, *strings.Builder) {
	buf := &strings.Builder{}
	ws := zapcore.AddSync(buf)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "" // omit timestamp for deterministic test output
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		ws,
		level,
	)

	return &Logger{logger: zap.New(core)}, buf
}

func TestLoggerNilReceiverFallsBackToNop(t *testing.T) {
	var nilLogger *Logger

	assert.NotPanics(t, func() {
		nilLogger.Info("message")
	})
}

func TestLoggerNilUnderlyingFallsBackToNop(t *testing.T) {
	logger := &Logger{}

	assert.NotPanics(t, func() {
		logger.Info("message")
	})
}

func TestStructuredLoggingMethods(t *testing.T) {
	logger, observed := newObservedLogger(zapcore.DebugLevel)

	logger.Debug("debug message")
	logger.Info("info message", String("request_id", "req-1"))
	logger.Warn("warn message")
	logger.Error("error message", ErrorField(errors.New("boom")))

	entries := observed.All()
	require.Len(t, entries, 4)

	assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
	assert.Equal(t, "debug message", entries[0].Message)

	assert.Equal(t, zapcore.InfoLevel, entries[1].Level)
	assert.Equal(t, "info message", entries[1].Message)
	assert.Equal(t, "req-1", entries[1].ContextMap()["request_id"])

	assert.Equal(t, zapcore.WarnLevel, entries[2].Level)
	assert.Equal(t, "warn message", entries[2].Message)

	assert.Equal(t, zapcore.ErrorLevel, entries[3].Level)
	assert.Equal(t, "error message", entries[3].Message)
}

func TestWithAddsFieldsWithoutMutatingParent(t *testing.T) {
	logger, observed := newObservedLogger(zapcore.DebugLevel)
	child := logger.With(String("tenant_id", "t-1"))

	logger.Info("parent")
	child.Info("child")

	entries := observed.All()
	require.Len(t, entries, 2)

	_, parentHasTenant := entries[0].ContextMap()["tenant_id"]
	assert.False(t, parentHasTenant)
	assert.Equal(t, "t-1", entries[1].ContextMap()["tenant_id"])
}

func TestSyncReturnsErrorFromUnderlyingLogger(t *testing.T) {
	logger, _ := newObservedLogger(zapcore.DebugLevel)

	assert.NoError(t, logger.Sync())
}

func TestFieldHelpers(t *testing.T) {
	logger, observed := newObservedLogger(zapcore.DebugLevel)
	logger.Info(
		"helpers",
		String("s", "value"),
		Int("i", 42),
		Bool("b", true),
		Duration("d", 2*time.Second),
	)

	entries := observed.All()
	require.Len(t, entries, 1)
	ctx := entries[0].ContextMap()

	assert.Equal(t, "value", ctx["s"])
	assert.Equal(t, int64(42), ctx["i"])
	assert.Equal(t, true, ctx["b"])
	assert.Equal(t, 2*time.Second, ctx["d"])
}

// ===========================================================================
// CWE-117: Log Injection Prevention for Zap Adapter
//
// Zap serializes output as JSON, which inherently escapes control characters
// in string values. These tests verify that behavior is preserved and that
// injection attempts cannot split log lines or forge entries.
// ===========================================================================

// TestCWE117_ZapMessageNewlineInjection verifies that newlines in log messages
// are properly escaped in JSON output, preventing log line splitting.
func TestCWE117_ZapMessageNewlineInjection(t *testing.T) {
	tests := []struct {
		name    string
		message string
	}{
		{
			name:    "LF in message",
			message: "legitimate\n{\"level\":\"error\",\"msg\":\"forged entry\"}",
		},
		{
			name:    "CR in message",
			message: "legitimate\r{\"level\":\"error\",\"msg\":\"forged entry\"}",
		},
		{
			name:    "CRLF in message",
			message: "legitimate\r\n{\"level\":\"error\",\"msg\":\"forged entry\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, buf := newBufferedLogger(zapcore.DebugLevel)
			logger.Info(tt.message)
			_ = logger.Sync()

			out := buf.String()
			// JSON output from zap should be a single line per entry
			lines := strings.Split(strings.TrimSpace(out), "\n")
			assert.Len(t, lines, 1,
				"CWE-117: zap JSON output must be a single line, got %d lines:\n%s", len(lines), out)

			// The raw newline characters should not appear in the JSON output
			// (JSON encoder escapes them as \n, \r)
			assert.NotContains(t, out, "forged entry\"}",
				"forged JSON entry must not appear as a separate parseable line")
		})
	}
}

// TestCWE117_ZapFieldValueInjection verifies field values with newlines
// are escaped by zap's JSON encoder.
func TestCWE117_ZapFieldValueInjection(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)

	maliciousValue := "user123\n{\"level\":\"error\",\"msg\":\"ADMIN ACCESS GRANTED\"}"
	logger.Info("login", String("user_id", maliciousValue))
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1,
		"CWE-117: field value injection must not create extra JSON lines")
}

// TestCWE117_ZapFieldNameInjection verifies that field names with control
// characters are escaped by zap's JSON encoder.
func TestCWE117_ZapFieldNameInjection(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)

	// Field name with embedded newline
	logger.Info("event", zap.String("key\ninjected", "value"))
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1,
		"CWE-117: field name injection must not create extra JSON lines")
}

// TestCWE117_ZapNullByteInMessage verifies null bytes in messages are handled.
func TestCWE117_ZapNullByteInMessage(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)
	logger.Info("before\x00after")
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1, "null byte must not split log output")
}

// TestCWE117_ZapANSIEscapeInMessage verifies ANSI escapes don't break output.
func TestCWE117_ZapANSIEscapeInMessage(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)
	logger.Info("normal \x1b[31mRED\x1b[0m normal")
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1, "ANSI escape must not split log output")
}

// TestCWE117_ZapTabInMessage verifies tab characters are handled in JSON output.
func TestCWE117_ZapTabInMessage(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)
	logger.Info("col1\tcol2\tcol3")
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1, "tabs must not split log output")
	// JSON encoder escapes tabs as \t in the JSON string
	assert.Contains(t, out, "col1")
	assert.Contains(t, out, "col2")
}

// TestCWE117_ZapWithPreservesSanitization verifies that child loggers created
// via With() still properly handle injection attempts.
func TestCWE117_ZapWithPreservesSanitization(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)
	child := logger.With(String("session", "sess\n{\"forged\":true}"))
	child.Info("child message")
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1,
		"CWE-117: With() must not allow field injection to split lines")
}

// TestCWE117_ZapMultipleVectorsSimultaneously combines multiple attack vectors.
func TestCWE117_ZapMultipleVectorsSimultaneously(t *testing.T) {
	logger, buf := newBufferedLogger(zapcore.DebugLevel)

	// Message with injection
	msg := "event\n{\"level\":\"error\",\"msg\":\"forged\"}\ttab\r\nmore"
	// Fields with injection
	logger.Info(msg,
		zap.String("user\nfake", "val\nfake"),
		zap.String("safe_key", "safe_val"))
	_ = logger.Sync()

	out := buf.String()
	lines := strings.Split(strings.TrimSpace(out), "\n")
	assert.Len(t, lines, 1,
		"CWE-117: combined attack vectors must not create multiple JSON lines")
}

// ===========================================================================
// Zap Level Filtering Tests
// ===========================================================================

// TestZapLevelFiltering verifies that the observed logger correctly filters
// by log level.
func TestZapLevelFiltering(t *testing.T) {
	t.Run("info level suppresses debug", func(t *testing.T) {
		logger, observed := newObservedLogger(zapcore.InfoLevel)
		logger.Debug("should be suppressed")
		logger.Info("should appear")

		entries := observed.All()
		require.Len(t, entries, 1)
		assert.Equal(t, "should appear", entries[0].Message)
	})

	t.Run("error level suppresses warn and below", func(t *testing.T) {
		logger, observed := newObservedLogger(zapcore.ErrorLevel)
		logger.Debug("suppressed")
		logger.Info("suppressed")
		logger.Warn("suppressed")
		logger.Error("visible")

		entries := observed.All()
		require.Len(t, entries, 1)
		assert.Equal(t, "visible", entries[0].Message)
	})
}

// TestZapRawReturnsUnderlyingLogger verifies Raw() returns the inner zap.Logger.
func TestZapRawReturnsUnderlyingLogger(t *testing.T) {
	logger, _ := newObservedLogger(zapcore.DebugLevel)
	raw := logger.Raw()
	assert.NotNil(t, raw)
}

// TestZapRawOnNilReturnsNop verifies Raw() on nil returns nop logger.
func TestZapRawOnNilReturnsNop(t *testing.T) {
	var logger *Logger
	raw := logger.Raw()
	assert.NotNil(t, raw, "Raw() on nil logger should return nop, not nil")
}

// TestZapErrorFieldHelper verifies the ErrorField helper.
func TestZapErrorFieldHelper(t *testing.T) {
	logger, observed := newObservedLogger(zapcore.DebugLevel)
	testErr := errors.New("test error")
	logger.Error("failed", ErrorField(testErr))

	entries := observed.All()
	require.Len(t, entries, 1)
	assert.Equal(t, "test error", entries[0].ContextMap()["error"].(string))
}

// TestZapAnyFieldHelper verifies the Any helper with various types.
func TestZapAnyFieldHelper(t *testing.T) {
	logger, observed := newObservedLogger(zapcore.DebugLevel)
	logger.Info("test",
		Any("slice", []string{"a", "b"}),
		Any("map", map[string]int{"x": 1}))

	entries := observed.All()
	require.Len(t, entries, 1)
	// Verify fields exist (exact format depends on zap encoding)
	ctx := entries[0].ContextMap()
	assert.NotNil(t, ctx["slice"])
	assert.NotNil(t, ctx["map"])
}
