package log

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafeErrorf_NilLogger(t *testing.T) {
	t.Parallel()

	SafeErrorf(nil, "test format", assert.AnError)
}

func TestSafeErrorf_NilError(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)
	original := currentProductionModeResolver(t)
	t.Cleanup(func() { SetProductionModeResolver(original) })

	logger := &GoLogger{Level: InfoLevel}
	resetProductionModeResolver(t, false)
	SafeErrorf(logger, "nil error test", nil)
	assert.Empty(t, buf.String())

	buf.Reset()
	resetProductionModeResolver(t, true)
	SafeErrorf(logger, "nil error test", nil)
	assert.Empty(t, buf.String())
}

func TestSafeErrorf_ProductionMode(t *testing.T) {
	var buf bytes.Buffer
	withTestLoggerOutput(t, &buf)

	logger := &GoLogger{Level: InfoLevel}
	original := currentProductionModeResolver(t)
	t.Cleanup(func() { SetProductionModeResolver(original) })
	resetProductionModeResolver(t, false)

	err := errors.New("credential_id=abc123")

	resetProductionModeResolver(t, false)
	SafeErrorf(logger, "request failed", err)
	assert.Contains(t, buf.String(), "credential_id=abc123")

	buf.Reset()
	resetProductionModeResolver(t, true)
	SafeErrorf(logger, "request failed", err)
	assert.Contains(t, buf.String(), "error_type=*")
	assert.NotContains(t, buf.String(), "credential_id=abc123")
}

func TestSanitizeExternalResponse(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		expected   string
	}{
		{
			name:       "400 Bad Request",
			statusCode: 400,
			expected:   "external system returned status 400",
		},
		{
			name:       "404 Not Found",
			statusCode: 404,
			expected:   "external system returned status 404",
		},
		{
			name:       "500 Internal Server Error",
			statusCode: 500,
			expected:   "external system returned status 500",
		},
		{
			name:       "503 Service Unavailable",
			statusCode: 503,
			expected:   "external system returned status 503",
		},
		{
			name:       "zero status code",
			statusCode: 0,
			expected:   "external system returned status 0",
		},
		{
			name:       "negative status code",
			statusCode: -1,
			expected:   "external system returned status -1",
		},
		{
			name:       "very large status code",
			statusCode: 99999,
			expected:   "external system returned status 99999",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeExternalResponse(tt.statusCode)
			assert.Equal(t, tt.expected, result)
			assert.False(t, strings.Contains(result, "body"))
			assert.False(t, strings.Contains(result, "response"))
		})
	}
}

func TestSetProductionModeResolver(t *testing.T) {
	original := currentProductionModeResolver(t)
	t.Cleanup(func() { SetProductionModeResolver(original) })

	resolverInvocations := 0
	SetProductionModeResolver(func() bool {
		resolverInvocations++
		return true
	})

	assert.True(t, isProductionMode())
	assert.Equal(t, 1, resolverInvocations)
}

func TestSetProductionModeResolver_NilIsIgnored(t *testing.T) {
	original := currentProductionModeResolver(t)
	t.Cleanup(func() { SetProductionModeResolver(original) })

	// Set a known resolver first.
	SetProductionModeResolver(func() bool { return false })
	assert.False(t, isProductionMode())

	// Passing nil should be a no-op — previous resolver must remain active.
	SetProductionModeResolver(nil)
	assert.False(t, isProductionMode())
}

func resetProductionModeResolver(t *testing.T, production bool) {
	t.Helper()
	if production {
		SetProductionModeResolver(func() bool { return true })
		return
	}

	SetProductionModeResolver(func() bool { return false })
}

func currentProductionModeResolver(t *testing.T) func() bool {
	t.Helper()
	productionModeMu.RLock()
	defer productionModeMu.RUnlock()

	return productionModeResolver
}
