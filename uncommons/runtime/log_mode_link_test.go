package runtime

import (
	"bytes"
	slog "log"
	"sync"
	"testing"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/stretchr/testify/assert"
)

var runtimeLoggerOutputMu sync.Mutex

func withRuntimeLoggerOutput(t *testing.T, output *bytes.Buffer) {
	t.Helper()

	runtimeLoggerOutputMu.Lock()
	defer t.Cleanup(func() {
		runtimeLoggerOutputMu.Unlock()
	})

	originalOutput := slog.Writer()
	slog.SetOutput(output)
	t.Cleanup(func() { slog.SetOutput(originalOutput) })
}

func TestLogProductionModeResolverRegistration(t *testing.T) {
	var buf bytes.Buffer
	withRuntimeLoggerOutput(t, &buf)

	logger := &log.GoLogger{Level: log.InfoLevel}
	initialMode := IsProductionMode()
	t.Cleanup(func() { SetProductionMode(initialMode) })

	SetProductionMode(false)
	log.SafeErrorf(logger, "runtime integration", assert.AnError)
	assert.Contains(t, buf.String(), "general error")

	buf.Reset()
	SetProductionMode(true)
	log.SafeErrorf(logger, "runtime integration", assert.AnError)
	assert.Contains(t, buf.String(), "error_type=*errors.errorString")
	assert.NotContains(t, buf.String(), "general error")
}
