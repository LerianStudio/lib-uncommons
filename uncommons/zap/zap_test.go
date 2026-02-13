package zap

import (
	"testing"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// Compile-time interface compliance check.
var _ log.Logger = (*ZapWithTraceLogger)(nil)

// newTestLogger creates an observable zap.SugaredLogger for assertions on log output.
// The observer captures all log entries so tests can verify message content, level, and fields.
func newTestLogger(level zapcore.Level) (*ZapWithTraceLogger, *observer.ObservedLogs) {
	core, obs := observer.New(level)
	sugar := zap.New(core).Sugar()

	return &ZapWithTraceLogger{
		Logger: sugar,
	}, obs
}

// newTestLoggerWithTemplate creates an observable logger with a default message template.
func newTestLoggerWithTemplate(level zapcore.Level, template string) (*ZapWithTraceLogger, *observer.ObservedLogs) {
	core, obs := observer.New(level)
	sugar := zap.New(core).Sugar()

	return &ZapWithTraceLogger{
		Logger:                 sugar,
		defaultMessageTemplate: template,
	}, obs
}

// --- hydrateArgs unit tests ---

func TestHydrateArgs(t *testing.T) {
	t.Run("prepends template to args", func(t *testing.T) {
		result := hydrateArgs("prefix: ", []any{"hello", "world"})
		require.Len(t, result, 3)
		assert.Equal(t, "prefix: ", result[0])
		assert.Equal(t, "hello", result[1])
		assert.Equal(t, "world", result[2])
	})

	t.Run("with empty template still prepends", func(t *testing.T) {
		result := hydrateArgs("", []any{"hello"})
		require.Len(t, result, 2)
		assert.Equal(t, "", result[0])
		assert.Equal(t, "hello", result[1])
	})

	t.Run("with nil args returns slice with only template", func(t *testing.T) {
		result := hydrateArgs("template", nil)
		require.Len(t, result, 1)
		assert.Equal(t, "template", result[0])
	})

	t.Run("with empty args returns slice with only template", func(t *testing.T) {
		result := hydrateArgs("template", []any{})
		require.Len(t, result, 1)
		assert.Equal(t, "template", result[0])
	})
}

// --- Nil-safety tests ---

func TestNilLoggerSafety(t *testing.T) {
	nilLogger := &ZapWithTraceLogger{}

	t.Run("Info does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Info("test") })
	})

	t.Run("Infof does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Infof("test %s", "val") })
	})

	t.Run("Infoln does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Infoln("test") })
	})

	t.Run("Error does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Error("test") })
	})

	t.Run("Errorf does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Errorf("test %s", "val") })
	})

	t.Run("Errorln does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Errorln("test") })
	})

	t.Run("Warn does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Warn("test") })
	})

	t.Run("Warnf does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Warnf("test %s", "val") })
	})

	t.Run("Warnln does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Warnln("test") })
	})

	t.Run("Debug does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Debug("test") })
	})

	t.Run("Debugf does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Debugf("test %s", "val") })
	})

	t.Run("Debugln does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.Debugln("test") })
	})

	t.Run("Sync returns nil with nil Logger", func(t *testing.T) {
		err := nilLogger.Sync()
		assert.NoError(t, err)
	})

	t.Run("WithFields returns self with nil Logger", func(t *testing.T) {
		result := nilLogger.WithFields("key", "value")
		assert.Same(t, nilLogger, result)
	})

	t.Run("WithDefaultMessageTemplate does not panic with nil Logger", func(t *testing.T) {
		assert.NotPanics(t, func() { nilLogger.WithDefaultMessageTemplate("template") })
	})
}

// --- Logging method behavioral tests ---

func TestInfoMethods(t *testing.T) {
	t.Run("Info logs message at info level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Info("hello world")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
		assert.Contains(t, entries[0].Message, "hello world")
	})

	t.Run("Infof logs formatted message at info level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Infof("count: %d", 42)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
		assert.Equal(t, "count: 42", entries[0].Message)
	})

	t.Run("Infoln logs message at info level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Infoln("hello", "world")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
	})
}

func TestErrorMethods(t *testing.T) {
	t.Run("Error logs message at error level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Error("something failed")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
		assert.Contains(t, entries[0].Message, "something failed")
	})

	t.Run("Errorf logs formatted message at error level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Errorf("error code: %d", 500)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
		assert.Equal(t, "error code: 500", entries[0].Message)
	})

	t.Run("Errorln logs message at error level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Errorln("error", "detail")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.ErrorLevel, entries[0].Level)
	})
}

func TestWarnMethods(t *testing.T) {
	t.Run("Warn logs message at warn level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Warn("be careful")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.WarnLevel, entries[0].Level)
		assert.Contains(t, entries[0].Message, "be careful")
	})

	t.Run("Warnf logs formatted message at warn level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Warnf("threshold: %d%%", 90)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.WarnLevel, entries[0].Level)
		assert.Equal(t, "threshold: 90%", entries[0].Message)
	})

	t.Run("Warnln logs message at warn level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Warnln("warning", "issued")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.WarnLevel, entries[0].Level)
	})
}

func TestDebugMethods(t *testing.T) {
	t.Run("Debug logs message at debug level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Debug("trace info")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
		assert.Contains(t, entries[0].Message, "trace info")
	})

	t.Run("Debugf logs formatted message at debug level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Debugf("step %d of %d", 3, 5)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
		assert.Equal(t, "step 3 of 5", entries[0].Message)
	})

	t.Run("Debugln logs message at debug level", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Debugln("debug", "detail")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, zapcore.DebugLevel, entries[0].Level)
	})
}

// --- Template hydration behavioral tests ---

func TestTemplateHydration(t *testing.T) {
	t.Run("Info without template does not prepend empty string", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Info("clean message")

		entries := obs.All()
		require.Len(t, entries, 1)
		// The key fix: no leading space when template is empty
		assert.Equal(t, "clean message", entries[0].Message)
	})

	t.Run("Info with template prepends template", func(t *testing.T) {
		zapLogger, obs := newTestLoggerWithTemplate(zapcore.DebugLevel, "REQ-123 | ")
		zapLogger.Info("processing request")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0].Message, "REQ-123 | ")
		assert.Contains(t, entries[0].Message, "processing request")
	})

	t.Run("Infof without template passes format unchanged", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)
		zapLogger.Infof("processed %d items", 100)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, "processed 100 items", entries[0].Message)
	})

	t.Run("Infof with template prepends template to format", func(t *testing.T) {
		zapLogger, obs := newTestLoggerWithTemplate(zapcore.DebugLevel, "REQ-456 | ")
		zapLogger.Infof("processed %d items in %dms", 100, 250)

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Equal(t, "REQ-456 | processed 100 items in 250ms", entries[0].Message)
	})
}

// --- Builder method tests ---

func TestWithFields(t *testing.T) {
	t.Run("returns new logger preserving template", func(t *testing.T) {
		zapLogger, _ := newTestLoggerWithTemplate(zapcore.DebugLevel, "template: ")
		newLogger := zapLogger.WithFields("key", "value")

		require.NotNil(t, newLogger)

		typed, ok := newLogger.(*ZapWithTraceLogger)
		require.True(t, ok, "WithFields should return *ZapWithTraceLogger")
		assert.Equal(t, "template: ", typed.defaultMessageTemplate)
		assert.NotSame(t, zapLogger.Logger, typed.Logger, "WithFields must return a new SugaredLogger instance")
	})

	t.Run("original logger is not mutated", func(t *testing.T) {
		zapLogger, obs := newTestLoggerWithTemplate(zapcore.DebugLevel, "")
		_ = zapLogger.WithFields("extra_key", "extra_value")

		// Log from the original — should NOT have the extra field
		zapLogger.Info("original")
		entries := obs.All()
		require.Len(t, entries, 1)

		for _, field := range entries[0].ContextMap() {
			assert.NotEqual(t, "extra_value", field)
		}
	})
}

func TestWithDefaultMessageTemplate(t *testing.T) {
	t.Run("returns new logger with updated template", func(t *testing.T) {
		zapLogger, _ := newTestLogger(zapcore.DebugLevel)
		newLogger := zapLogger.WithDefaultMessageTemplate("new-template: ")

		require.NotNil(t, newLogger)

		typed, ok := newLogger.(*ZapWithTraceLogger)
		require.True(t, ok)
		assert.Equal(t, "new-template: ", typed.defaultMessageTemplate)
	})

	t.Run("original logger template unchanged", func(t *testing.T) {
		zapLogger, _ := newTestLoggerWithTemplate(zapcore.DebugLevel, "original: ")
		_ = zapLogger.WithDefaultMessageTemplate("new: ")

		assert.Equal(t, "original: ", zapLogger.defaultMessageTemplate)
	})

	t.Run("shares underlying Logger instance", func(t *testing.T) {
		zapLogger, _ := newTestLogger(zapcore.DebugLevel)
		newLogger := zapLogger.WithDefaultMessageTemplate("prefix: ")

		typed := newLogger.(*ZapWithTraceLogger)
		assert.Same(t, zapLogger.Logger, typed.Logger, "WithDefaultMessageTemplate should share the same SugaredLogger")
	})
}

func TestSync(t *testing.T) {
	t.Run("returns nil on success", func(t *testing.T) {
		zapLogger, _ := newTestLogger(zapcore.DebugLevel)
		err := zapLogger.Sync()
		assert.NoError(t, err)
	})
}
