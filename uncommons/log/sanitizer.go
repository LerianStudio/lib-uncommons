package log

import (
	"context"
	"fmt"
)

// SafeError logs errors with explicit production-aware sanitization.
// When production is true, only the error type is logged.
func SafeError(logger Logger, ctx context.Context, msg string, err error, production bool) {
	if logger == nil {
		return
	}

	if err == nil {
		return
	}

	if !logger.Enabled(LevelError) {
		return
	}

	if production {
		logger.Log(ctx, LevelError, msg, String("error_type", fmt.Sprintf("%T", err)))
		return
	}

	logger.Log(ctx, LevelError, msg, Err(err))
}

// SanitizeExternalResponse removes potentially sensitive external response data.
// Returns only status code for error messages.
func SanitizeExternalResponse(statusCode int) string {
	return fmt.Sprintf("external system returned status %d", statusCode)
}
