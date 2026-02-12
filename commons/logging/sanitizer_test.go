//go:build unit

package logging

import (
	"strings"
	"testing"

	"github.com/LerianStudio/lib-commons-v2/v3/commons/runtime"
	"github.com/stretchr/testify/assert"
)

func TestSafeErrorf_NilLogger(t *testing.T) {
	t.Parallel()

	// Should not panic with nil logger
	SafeErrorf(nil, "test format", assert.AnError)
}

func TestSanitizeExternalResponse(t *testing.T) {
	t.Parallel()

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := SanitizeExternalResponse(tt.statusCode)
			assert.Equal(t, tt.expected, result)
			// Verify no PII-like content
			assert.False(t, strings.Contains(result, "body"))
			assert.False(t, strings.Contains(result, "response"))
		})
	}
}

//nolint:paralleltest // Cannot use t.Parallel() - modifies global productionMode
func TestIsProductionMode_Integration(t *testing.T) {
	// Verify IsProductionMode is callable (integration with runtime package)

	// Save current state
	initialMode := runtime.IsProductionMode()
	defer runtime.SetProductionMode(initialMode)

	// Test non-production mode
	runtime.SetProductionMode(false)
	assert.False(t, runtime.IsProductionMode())

	// Test production mode
	runtime.SetProductionMode(true)
	assert.True(t, runtime.IsProductionMode())
}
