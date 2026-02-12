// Package logging provides production-safe logging utilities.
// It includes functions for sanitizing error messages and external responses
// to prevent PII (Personally Identifiable Information) exposure in logs.
package logging

import (
	"fmt"

	libLog "github.com/LerianStudio/lib-commons-v2/v3/commons/log"

	"github.com/LerianStudio/lib-commons-v2/v3/commons/runtime"
)

// SafeErrorf logs errors with production-aware sanitization.
// In production, only logs error type to prevent PII exposure.
func SafeErrorf(logger libLog.Logger, format string, err error) {
	if logger == nil {
		return
	}

	if runtime.IsProductionMode() {
		logger.Errorf("%s: error_type=%T", format, err)
	} else {
		logger.Errorf("%s: %v", format, err)
	}
}

// SanitizeExternalResponse removes potentially sensitive external response data.
// Returns only status code for error messages.
func SanitizeExternalResponse(statusCode int) string {
	return fmt.Sprintf("external system returned status %d", statusCode)
}
