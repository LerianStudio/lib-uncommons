package log

import (
	"fmt"
	"sync"
)

// SafeErrorf logs errors with production-aware sanitization.
// In production, only the error type (e.g., *net.OpError) is logged to prevent PII exposure.
// This is a deliberate trade-off: error types may reveal infrastructure details, but they
// are essential for operational diagnostics without leaking user data.
//
// Production mode is determined via isProductionMode(), which acquires a sync.RWMutex read lock.
// Under extreme concurrency this adds negligible overhead — RLock is non-exclusive and only
// contends with the rare write path (SetProductionModeResolver).
func SafeErrorf(logger Logger, format string, err error) {
	if logger == nil {
		return
	}

	if err == nil {
		return
	}

	if isProductionMode() {
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

var (
	// Default to safe behavior when runtime package wiring is not initialized.
	defaultProductionModeCheck = func() bool { return true }
	productionModeMu           sync.RWMutex
	productionModeResolver     func() bool = defaultProductionModeCheck
)

// SetProductionModeResolver configures how SafeErrorf determines production mode.
// This indirection avoids importing the runtime package directly from log.
func SetProductionModeResolver(check func() bool) {
	if check == nil {
		return
	}

	productionModeMu.Lock()
	defer productionModeMu.Unlock()

	productionModeResolver = check
}

func isProductionMode() bool {
	productionModeMu.RLock()
	defer productionModeMu.RUnlock()

	return productionModeResolver()
}
