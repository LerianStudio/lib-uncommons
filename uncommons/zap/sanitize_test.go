package zap

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestSanitizeString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"clean string unchanged", "hello world", "hello world"},
		{"newline escaped", "line1\nline2", `line1\nline2`},
		{"carriage return escaped", "line1\rline2", `line1\rline2`},
		{"tab escaped", "col1\tcol2", `col1\tcol2`},
		{"mixed control chars escaped", "a\nb\rc\td", `a\nb\rc\td`},
		{"empty string unchanged", "", ""},
		{"no false positives on backslash-n literal", `already\nescaped`, `already\nescaped`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, sanitizeString(tc.input))
		})
	}
}

func TestSanitizeArgs(t *testing.T) {
	t.Parallel()

	t.Run("string args sanitized", func(t *testing.T) {
		t.Parallel()

		args := []any{"clean", "has\nnewline", 42, true}
		result := sanitizeArgs(args)

		require.Len(t, result, 4)
		assert.Equal(t, "clean", result[0])
		assert.Equal(t, `has\nnewline`, result[1])
		assert.Equal(t, 42, result[2])
		assert.Equal(t, true, result[3])
	})

	t.Run("nil args returns empty slice", func(t *testing.T) {
		t.Parallel()

		result := sanitizeArgs(nil)
		assert.Empty(t, result)
		assert.NotNil(t, result)
	})

	t.Run("empty args returns empty slice", func(t *testing.T) {
		t.Parallel()

		result := sanitizeArgs([]any{})
		assert.Empty(t, result)
	})

	t.Run("non-string types pass through", func(t *testing.T) {
		t.Parallel()

		err := fmt.Errorf("error with\nnewline")
		args := []any{42, 3.14, err, nil}
		result := sanitizeArgs(args)

		require.Len(t, result, 4)
		assert.Equal(t, 42, result[0])
		assert.Equal(t, 3.14, result[1])
		assert.Same(t, err, result[2].(error)) //nolint:errorlint // intentional identity check
		assert.Nil(t, result[3])
	})
}

func TestSanitizeFormat(t *testing.T) {
	t.Parallel()

	t.Run("format verbs preserved", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "count: %d, name: %s", sanitizeFormat("count: %d, name: %s"))
	})

	t.Run("newlines in format escaped", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, `line1\nline2: %s`, sanitizeFormat("line1\nline2: %s"))
	})
}

// TestLogInjectionPrevention is an integration test that verifies the full attack vector
// described in the review (CWE-117, OWASP A03:2021) is mitigated end-to-end.
func TestLogInjectionPrevention(t *testing.T) {
	t.Run("Info with injected newline does not produce multiple log entries", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)

		// Simulated attack: user input contains a fake log line
		malicious := "normal\n{\"level\":\"info\",\"msg\":\"admin login successful\",\"user\":\"admin\"}"
		zapLogger.Info(malicious)

		entries := obs.All()
		require.Len(t, entries, 1, "injected newline must not create a second log entry")
		assert.Contains(t, entries[0].Message, `\n`)
		assert.NotContains(t, entries[0].Message, "\n", "raw newline must be escaped")
	})

	t.Run("Infof with injected newline in format string", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)

		zapLogger.Infof("user said: %s\nINJECTED", "value")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.NotContains(t, entries[0].Message, "\n")
	})

	t.Run("Infof with injected newline in argument", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)

		zapLogger.Infof("user said: %s", "value\nINJECTED")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.Contains(t, entries[0].Message, `\n`)
	})

	t.Run("carriage return injection prevented", func(t *testing.T) {
		zapLogger, obs := newTestLogger(zapcore.DebugLevel)

		zapLogger.Info("start\r\nfake entry")

		entries := obs.All()
		require.Len(t, entries, 1)
		assert.NotContains(t, entries[0].Message, "\r")
		assert.NotContains(t, entries[0].Message, "\n")
	})
}
