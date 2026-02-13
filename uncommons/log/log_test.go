package log

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var stdLoggerOutputMu sync.Mutex

// withTestLoggerOutput redirects the global log.Writer() to the given buffer for the
// duration of the test. It serializes access via stdLoggerOutputMu to prevent concurrent
// tests from stomping on each other's output.
//
// IMPORTANT: Tests that call this helper MUST NOT use t.Parallel(). The mutex is held for
// the full test lifetime (released via t.Cleanup). Adding t.Parallel() would allow the
// Unlock cleanup to race with other tests acquiring the lock, breaking the safety invariant.
func withTestLoggerOutput(t *testing.T, output *bytes.Buffer) {
	t.Helper()

	stdLoggerOutputMu.Lock()
	defer t.Cleanup(func() {
		stdLoggerOutputMu.Unlock()
	})

	originalOutput := log.Writer()
	log.SetOutput(output)
	t.Cleanup(func() { log.SetOutput(originalOutput) })
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    LogLevel
		expectError bool
	}{
		{
			name:        "parse fatal level",
			input:       "fatal",
			expected:    FatalLevel,
			expectError: false,
		},
		{
			name:        "parse error level",
			input:       "error",
			expected:    ErrorLevel,
			expectError: false,
		},
		{
			name:        "parse warn level",
			input:       "warn",
			expected:    WarnLevel,
			expectError: false,
		},
		{
			name:        "parse warning level",
			input:       "warning",
			expected:    WarnLevel,
			expectError: false,
		},
		{
			name:        "parse info level",
			input:       "info",
			expected:    InfoLevel,
			expectError: false,
		},
		{
			name:        "parse debug level",
			input:       "debug",
			expected:    DebugLevel,
			expectError: false,
		},
		{
			name:        "parse uppercase level",
			input:       "INFO",
			expected:    InfoLevel,
			expectError: false,
		},
		{
			name:        "parse mixed case level",
			input:       "WaRn",
			expected:    WarnLevel,
			expectError: false,
		},
		{
			name:        "parse invalid level",
			input:       "invalid",
			expected:    LogLevel(0),
			expectError: true,
		},
		{
			name:        "parse empty string",
			input:       "",
			expected:    LogLevel(0),
			expectError: true,
		},
		{
			name:        "parse panic level",
			input:       "panic",
			expected:    PanicLevel,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level, err := ParseLevel(tt.input)

			if tt.expectError {
				assert.EqualError(t, err, "not a valid LogLevel: \""+tt.input+"\"")
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, level)
			}
		})
	}
}

func TestGoLogger_IsLevelEnabled(t *testing.T) {
	tests := []struct {
		name        string
		loggerLevel LogLevel
		checkLevel  LogLevel
		expected    bool
	}{
		{
			name:        "debug logger - check debug",
			loggerLevel: DebugLevel,
			checkLevel:  DebugLevel,
			expected:    true,
		},
		{
			name:        "debug logger - check info",
			loggerLevel: DebugLevel,
			checkLevel:  InfoLevel,
			expected:    true,
		},
		{
			name:        "info logger - check debug",
			loggerLevel: InfoLevel,
			checkLevel:  DebugLevel,
			expected:    false,
		},
		{
			name:        "info logger - check info",
			loggerLevel: InfoLevel,
			checkLevel:  InfoLevel,
			expected:    true,
		},
		{
			name:        "error logger - check warn",
			loggerLevel: ErrorLevel,
			checkLevel:  WarnLevel,
			expected:    false,
		},
		{
			name:        "error logger - check error",
			loggerLevel: ErrorLevel,
			checkLevel:  ErrorLevel,
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := &GoLogger{Level: tt.loggerLevel}
			result := logger.IsLevelEnabled(tt.checkLevel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGoLogger_Info(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	tests := []struct {
		name         string
		loggerLevel  LogLevel
		message      string
		expectLogged bool
	}{
		{
			name:         "info level - log info",
			loggerLevel:  InfoLevel,
			message:      "test info message",
			expectLogged: true,
		},
		{
			name:         "warn level - log info",
			loggerLevel:  WarnLevel,
			message:      "test info message",
			expectLogged: false,
		},
		{
			name:         "debug level - log info",
			loggerLevel:  DebugLevel,
			message:      "test info message",
			expectLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			logger := &GoLogger{Level: tt.loggerLevel}

			logger.Info(tt.message)

			output := buf.String()
			if tt.expectLogged {
				assert.Contains(t, output, tt.message)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestGoLogger_Infof(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	buf.Reset()
	logger.Infof("test %s message %d", "formatted", 123)

	output := buf.String()
	assert.Contains(t, output, "test formatted message 123")
}

func TestGoLogger_Infoln(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	buf.Reset()
	logger.Infoln("test", "info", "line")

	output := buf.String()
	assert.Contains(t, output, "test info line")
}

func TestGoLogger_Error(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	tests := []struct {
		name         string
		loggerLevel  LogLevel
		message      string
		expectLogged bool
	}{
		{
			name:         "error level - log error",
			loggerLevel:  ErrorLevel,
			message:      "test error message",
			expectLogged: true,
		},
		{
			name:         "fatal level - log error",
			loggerLevel:  FatalLevel,
			message:      "test error message",
			expectLogged: false,
		},
		{
			name:         "debug level - log error",
			loggerLevel:  DebugLevel,
			message:      "test error message",
			expectLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			logger := &GoLogger{Level: tt.loggerLevel}

			logger.Error(tt.message)

			output := buf.String()
			if tt.expectLogged {
				assert.Contains(t, output, tt.message)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestGoLogger_Warn(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	tests := []struct {
		name         string
		loggerLevel  LogLevel
		message      string
		expectLogged bool
	}{
		{
			name:         "warn level - log warn",
			loggerLevel:  WarnLevel,
			message:      "test warn message",
			expectLogged: true,
		},
		{
			name:         "error level - log warn",
			loggerLevel:  ErrorLevel,
			message:      "test warn message",
			expectLogged: false,
		},
		{
			name:         "info level - log warn",
			loggerLevel:  InfoLevel,
			message:      "test warn message",
			expectLogged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			logger := &GoLogger{Level: tt.loggerLevel}

			logger.Warn(tt.message)

			output := buf.String()
			if tt.expectLogged {
				assert.Contains(t, output, tt.message)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestGoLogger_Debug(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	tests := []struct {
		name         string
		loggerLevel  LogLevel
		message      string
		expectLogged bool
	}{
		{
			name:         "debug level - log debug",
			loggerLevel:  DebugLevel,
			message:      "test debug message",
			expectLogged: true,
		},
		{
			name:         "info level - log debug",
			loggerLevel:  InfoLevel,
			message:      "test debug message",
			expectLogged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			logger := &GoLogger{Level: tt.loggerLevel}

			logger.Debug(tt.message)

			output := buf.String()
			if tt.expectLogged {
				assert.Contains(t, output, tt.message)
			} else {
				assert.Empty(t, output)
			}
		})
	}
}

func TestGoLogger_WithFields(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	buf.Reset()
	loggerWithFields := logger.WithFields("key1", "value1", "key2", 123)
	loggerWithMoreFields := loggerWithFields.WithFields("key3", "value3")
	loggerWithMoreFields.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "[info]")
	assert.Contains(t, output, "test message")
	assert.Contains(t, output, "[key1=value1")
	assert.Contains(t, output, "key2=123")
	assert.Contains(t, output, "key3=value3")

	assert.Equal(t, []any{"key1", "value1", "key2", 123}, loggerWithFields.(*GoLogger).fields)
	assert.Equal(t, []any{"key1", "value1", "key2", 123, "key3", "value3"}, loggerWithMoreFields.(*GoLogger).fields)

	// Verify original logger is not modified
	buf.Reset()
	logger.Info("original logger")
	output = buf.String()
	assert.Contains(t, output, "original logger")

	// Verify WithFields returns a new logger instance
	assert.NotEqual(t, logger, loggerWithFields)
}

func TestGoLogger_WithDefaultMessageTemplate(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	buf.Reset()
	loggerWithTemplate := logger.WithDefaultMessageTemplate("Template: ")
	loggerWithTemplate.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "[info]")
	assert.Contains(t, output, "Template:")
	assert.Contains(t, output, "test message")

	assert.Equal(t, "Template: ", loggerWithTemplate.(*GoLogger).defaultMessageTemplate)

	// Verify original logger is not modified (immutability)
	buf.Reset()
	logger.Info("original message")
	output = buf.String()
	assert.Contains(t, output, "original message")
	assert.NotContains(t, output, "Template:")
}

func TestGoLogger_Sync(t *testing.T) {
	logger := &GoLogger{Level: InfoLevel}
	err := logger.Sync()
	assert.NoError(t, err)
}

func TestGoLogger_FormattedMethods(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: DebugLevel}

	// Test Errorf
	buf.Reset()
	logger.Errorf("error: %s %d", "test", 42)
	assert.Contains(t, buf.String(), "error: test 42")

	// Test Warnf
	buf.Reset()
	logger.Warnf("warning: %s %d", "test", 42)
	assert.Contains(t, buf.String(), "warning: test 42")

	// Test Debugf
	buf.Reset()
	logger.Debugf("debug: %s %d", "test", 42)
	assert.Contains(t, buf.String(), "debug: test 42")
}

func TestGoLogger_LineMethods(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: DebugLevel}

	// Test Errorln
	buf.Reset()
	logger.Errorln("error", "line", "test")
	assert.Contains(t, buf.String(), "error line test")

	// Test Warnln
	buf.Reset()
	logger.Warnln("warn", "line", "test")
	assert.Contains(t, buf.String(), "warn line test")

	// Test Debugln
	buf.Reset()
	logger.Debugln("debug", "line", "test")
	assert.Contains(t, buf.String(), "debug line test")
}

func TestNoneLogger(t *testing.T) {
	// NoneLogger should not panic and should return itself for chaining methods
	logger := &NoneLogger{}

	t.Run("info methods do not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			logger.Info("test")
			logger.Infof("test %s", "format")
			logger.Infoln("test", "line")
		})
	})

	t.Run("error methods do not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			logger.Error("test")
			logger.Errorf("test %s", "format")
			logger.Errorln("test", "line")
		})
	})

	t.Run("warn methods do not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			logger.Warn("test")
			logger.Warnf("test %s", "format")
			logger.Warnln("test", "line")
		})
	})

	t.Run("debug methods do not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			logger.Debug("test")
			logger.Debugf("test %s", "format")
			logger.Debugln("test", "line")
		})
	})

	t.Run("fatal methods do not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			logger.Fatal("test")
			logger.Fatalf("test %s", "format")
			logger.Fatalln("test", "line")
		})
	})

	t.Run("WithFields returns itself and is chainable", func(t *testing.T) {
		result := logger.WithFields("key", "value")
		assert.Same(t, logger, result)
		assert.IsType(t, &NoneLogger{}, result)

		// Chain further — still returns same instance.
		chained := result.WithFields("key2", "value2")
		assert.Same(t, logger, chained)
	})

	t.Run("WithDefaultMessageTemplate returns itself and is chainable", func(t *testing.T) {
		result := logger.WithDefaultMessageTemplate("template")
		assert.Same(t, logger, result)
		assert.IsType(t, &NoneLogger{}, result)
	})

	t.Run("Sync returns nil", func(t *testing.T) {
		err := logger.Sync()
		assert.NoError(t, err)
	})
}

func TestGoLogger_ComplexScenarios(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	// Test chaining methods
	logger := &GoLogger{Level: InfoLevel}

	buf.Reset()
	loggerWithFields := logger.WithFields("request_id", "123", "user_id", "456")
	loggerWithTemplate := loggerWithFields.WithDefaultMessageTemplate("[request] ")
	loggerWithTemplate.Info("API: request processed")

	output := buf.String()
	assert.Contains(t, output, "request_id")
	assert.Contains(t, output, "123")
	assert.Contains(t, output, "user_id")
	assert.Contains(t, output, "456")
	assert.Contains(t, output, "[request]")
	assert.Contains(t, output, "API: request processed")

	// Test multiple arguments
	buf.Reset()
	logger.Info("multiple", "arguments", 123, true, 45.67)
	output = buf.String()
	assert.Contains(t, output, "multiple")
	assert.Contains(t, output, "arguments")
	assert.Contains(t, output, "123")
	assert.Contains(t, output, "true")
	assert.Contains(t, output, "45.67")
}

func TestLogLevel_String(t *testing.T) {
	// Test that log levels have proper string representations
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{PanicLevel, "panic"},
		{FatalLevel, "fatal"},
		{ErrorLevel, "error"},
		{WarnLevel, "warn"},
		{InfoLevel, "info"},
		{DebugLevel, "debug"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.level.String())
			parsed, err := ParseLevel(tt.expected)
			assert.NoError(t, err)
			assert.Equal(t, tt.level, parsed)
		})
	}

	assert.Equal(t, "unknown", LogLevel(127).String())
}

func TestGoLogger_Fatal_LevelDisabled(t *testing.T) {
	// When FatalLevel is disabled (level set lower), Fatal should be a no-op.
	// Level 0 = PanicLevel, which is less than FatalLevel (1), so Fatal is disabled.
	logger := &GoLogger{Level: 0}

	// Should NOT call log.Fatal (which would exit) — just a no-op
	assert.NotPanics(t, func() {
		logger.Fatal("this should not cause exit")
	})
	assert.NotPanics(t, func() {
		logger.Fatalf("this should not cause exit: %s", "test")
	})
	assert.NotPanics(t, func() {
		logger.Fatalln("this should not cause exit")
	})
}

func TestGoLogger_Fatal_CallsLogFatal(t *testing.T) {
	// Test that Fatal actually calls log.Fatal (which exits) when level is enabled.
	// Uses subprocess pattern since log.Fatal calls os.Exit(1).
	if os.Getenv("TEST_FATAL_EXIT") == "1" {
		logger := &GoLogger{Level: FatalLevel}
		logger.Fatal("fatal test message")
		return // should never reach here
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestGoLogger_Fatal_CallsLogFatal")
	cmd.Env = append(os.Environ(), "TEST_FATAL_EXIT=1")
	err := cmd.Run()

	// The process should have exited with code 1
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode(), "log.Fatal should cause exit code 1")
}

func TestGoLogger_OutputIncludesLevelPrefix(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: DebugLevel}

	buf.Reset()
	logger.Info("info")
	assert.Contains(t, buf.String(), "[info]")
	assert.NotContains(t, buf.String(), "[error]")

	buf.Reset()
	logger.Warn("warn")
	assert.Contains(t, buf.String(), "[warn]")

	buf.Reset()
	logger.Error("error")
	assert.Contains(t, buf.String(), "[error]")

	buf.Reset()
	logger.Debug("debug")
	assert.Contains(t, buf.String(), "[debug]")
}

func TestGoLogger_Fatalf_CallsLogFatalf(t *testing.T) {
	if os.Getenv("TEST_FATALF_EXIT") == "1" {
		logger := &GoLogger{Level: FatalLevel}
		logger.Fatalf("fatal: %s", "formatted")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestGoLogger_Fatalf_CallsLogFatalf")
	cmd.Env = append(os.Environ(), "TEST_FATALF_EXIT=1")
	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode(), "log.Fatalf should cause exit code 1")
}

func TestGoLogger_Fatalln_CallsLogFatalln(t *testing.T) {
	if os.Getenv("TEST_FATALLN_EXIT") == "1" {
		logger := &GoLogger{Level: FatalLevel}
		logger.Fatalln("fatal line")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestGoLogger_Fatalln_CallsLogFatalln")
	cmd.Env = append(os.Environ(), "TEST_FATALLN_EXIT=1")
	err := cmd.Run()

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode(), "log.Fatalln should cause exit code 1")
}

func TestGoLogger_NilReceiver(t *testing.T) {
	var nilLogger *GoLogger

	t.Run("IsLevelEnabled returns false on nil receiver", func(t *testing.T) {
		assert.False(t, nilLogger.IsLevelEnabled(InfoLevel))
		assert.False(t, nilLogger.IsLevelEnabled(DebugLevel))
	})

	t.Run("log methods on nil receiver do not panic", func(t *testing.T) {
		methods := []struct {
			name string
			call func()
		}{
			{name: "Info", call: func() { nilLogger.Info("test") }},
			{name: "Infof", call: func() { nilLogger.Infof("test %s", "val") }},
			{name: "Infoln", call: func() { nilLogger.Infoln("test") }},
			{name: "Error", call: func() { nilLogger.Error("test") }},
			{name: "Errorf", call: func() { nilLogger.Errorf("test %s", "val") }},
			{name: "Errorln", call: func() { nilLogger.Errorln("test") }},
			{name: "Warn", call: func() { nilLogger.Warn("test") }},
			{name: "Warnf", call: func() { nilLogger.Warnf("test %s", "val") }},
			{name: "Warnln", call: func() { nilLogger.Warnln("test") }},
			{name: "Debug", call: func() { nilLogger.Debug("test") }},
			{name: "Debugf", call: func() { nilLogger.Debugf("test %s", "val") }},
			{name: "Debugln", call: func() { nilLogger.Debugln("test") }},
			{name: "Fatal", call: func() { nilLogger.Fatal("test") }},
			{name: "Fatalf", call: func() { nilLogger.Fatalf("test %s", "val") }},
			{name: "Fatalln", call: func() { nilLogger.Fatalln("test") }},
		}

		for _, tt := range methods {
			t.Run(tt.name, func(t *testing.T) {
				assert.NotPanics(t, tt.call)
			})
		}
	})

	t.Run("WithFields on nil receiver returns usable zero-value logger", func(t *testing.T) {
		result := nilLogger.WithFields("key", "value")
		assert.NotNil(t, result)

		goLogger, ok := result.(*GoLogger)
		assert.True(t, ok)
		assert.NotNil(t, goLogger)
		// Zero-value GoLogger: Level=PanicLevel(0), no fields — all methods are no-ops.
		assert.False(t, goLogger.IsLevelEnabled(InfoLevel))
	})

	t.Run("WithDefaultMessageTemplate on nil receiver returns usable zero-value logger", func(t *testing.T) {
		result := nilLogger.WithDefaultMessageTemplate("template")
		assert.NotNil(t, result)

		goLogger, ok := result.(*GoLogger)
		assert.True(t, ok)
		assert.NotNil(t, goLogger)
	})

	t.Run("Sync on nil receiver returns nil", func(t *testing.T) {
		assert.Nil(t, nilLogger.Sync())
	})
}

func TestGoLogger_WithFields_OddArgCount(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	// Odd number of field args — the orphan key should appear without panicking.
	buf.Reset()
	loggerOrphan := logger.WithFields("orphan_key")
	loggerOrphan.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "orphan_key")
	assert.Contains(t, output, "test message")

	// Three args: first pair is key=value, third is orphan.
	buf.Reset()
	loggerMixed := logger.WithFields("key", "value", "orphan")
	loggerMixed.Info("mixed")

	output = buf.String()
	assert.Contains(t, output, "key=value")
	assert.Contains(t, output, "orphan")
	assert.Contains(t, output, "mixed")
}

func TestGoLogger_EdgeCases(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}

	// Test with nil arguments
	buf.Reset()
	logger.Info(nil)
	assert.Contains(t, buf.String(), "<nil>")

	// Test with empty string
	buf.Reset()
	logger.Info("")
	// Empty string still produces output with timestamp
	assert.NotEmpty(t, buf.String())

	// Test with special characters — CWE-117 sanitization escapes control chars.
	buf.Reset()
	logger.Info("special chars: \n\t\r")
	output := buf.String()
	assert.Contains(t, output, "special chars:")
	// Control characters should be escaped, not present as literal bytes.
	assert.Contains(t, output, `\n`)
	assert.Contains(t, output, `\t`)
	assert.Contains(t, output, `\r`)

	// Test format with wrong number of arguments
	buf.Reset()
	logger.Infof("format %s", "only one arg")
	output = buf.String()
	assert.Contains(t, output, "format only one arg")
}

func TestGoLogger_CWE117_LogInjection(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: DebugLevel}

	t.Run("Info escapes newlines in arguments", func(t *testing.T) {
		buf.Reset()
		logger.Info("normal line\ninjected log entry")
		output := buf.String()
		assert.Contains(t, output, `normal line\ninjected log entry`)
		// Should NOT contain a literal newline within the message (only the trailing one from log.Print).
		lines := strings.Split(strings.TrimSpace(output), "\n")
		assert.Len(t, lines, 1, "log injection should not create multiple log lines")
	})

	t.Run("Infof escapes newlines in format string", func(t *testing.T) {
		buf.Reset()
		logger.Infof("user: %s\nfake-level: CRITICAL", "attacker")
		output := buf.String()
		assert.Contains(t, output, `\n`)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		assert.Len(t, lines, 1)
	})

	t.Run("Infoln escapes control chars in arguments", func(t *testing.T) {
		buf.Reset()
		logger.Infoln("msg\rwith\tcontrol\nchars")
		output := buf.String()
		assert.Contains(t, output, `\r`)
		assert.Contains(t, output, `\t`)
		assert.Contains(t, output, `\n`)
	})

	t.Run("non-string arguments pass through unchanged", func(t *testing.T) {
		buf.Reset()
		logger.Info(42, true, 3.14)
		output := buf.String()
		assert.Contains(t, output, "42")
		assert.Contains(t, output, "true")
		assert.Contains(t, output, "3.14")
	})
}
