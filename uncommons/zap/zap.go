package zap

import (
	"time"

	"go.uber.org/zap"
)

// Field is a typed structured logging field.
type Field = zap.Field

// Logger is a strict structured logger.
//
// It intentionally does not expose printf/line/fatal helpers.
type Logger struct {
	logger *zap.Logger
}

func (l *Logger) must() *zap.Logger {
	if l == nil || l.logger == nil {
		return zap.NewNop()
	}

	return l.logger
}

// With returns a child logger with additional structured fields.
func (l *Logger) With(fields ...Field) *Logger {
	return &Logger{logger: l.must().With(fields...)}
}

// Debug logs a message with debug severity.
func (l *Logger) Debug(message string, fields ...Field) {
	l.must().Debug(message, fields...)
}

// Info logs a message with info severity.
func (l *Logger) Info(message string, fields ...Field) {
	l.must().Info(message, fields...)
}

// Warn logs a message with warn severity.
func (l *Logger) Warn(message string, fields ...Field) {
	l.must().Warn(message, fields...)
}

// Error logs a message with error severity.
func (l *Logger) Error(message string, fields ...Field) {
	l.must().Error(message, fields...)
}

// Sync flushes buffered logs.
func (l *Logger) Sync() error {
	return l.must().Sync()
}

// Raw returns the underlying zap logger.
func (l *Logger) Raw() *zap.Logger {
	return l.must()
}

// Any creates a field with any value.
func Any(key string, value any) Field {
	return zap.Any(key, value)
}

// String creates a string field.
func String(key, value string) Field {
	return zap.String(key, value)
}

// Int creates an int field.
func Int(key string, value int) Field {
	return zap.Int(key, value)
}

// Bool creates a bool field.
func Bool(key string, value bool) Field {
	return zap.Bool(key, value)
}

// Duration creates a duration field.
func Duration(key string, value time.Duration) Field {
	return zap.Duration(key, value)
}

// ErrorField creates an error field.
func ErrorField(err error) Field {
	return zap.Error(err)
}
