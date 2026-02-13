package log

import (
	"context"
	"fmt"
	"strings"
)

// Logger is the package interface for v2 logging.
//
//go:generate mockgen --destination=log_mock.go --package=log . Logger
type Logger interface {
	Log(ctx context.Context, level Level, msg string, fields ...Field)
	With(fields ...Field) Logger
	WithGroup(name string) Logger
	Enabled(level Level) bool
	Sync(ctx context.Context) error
}

// Level represents the severity of a log entry.
type Level uint8

// These are the supported log levels.
const (
	LevelError Level = iota
	LevelWarn
	LevelInfo
	LevelDebug
)

// String returns the string representation of a log level.
func (level Level) String() string {
	switch level {
	case LevelDebug:
		return "debug"
	case LevelInfo:
		return "info"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "unknown"
	}
}

// ParseLevel takes a string level and returns a Level constant.
func ParseLevel(lvl string) (Level, error) {
	switch strings.ToLower(lvl) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	}

	var l Level

	return l, fmt.Errorf("not a valid Level: %q", lvl)
}

// Field is a strongly-typed key/value attribute attached to a log event.
type Field struct {
	Key   string
	Value any
}

// Any creates a field with an arbitrary value.
func Any(key string, value any) Field {
	return Field{Key: key, Value: value}
}

// String creates a string field.
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

// Int creates an integer field.
func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

// Bool creates a boolean field.
func Bool(key string, value bool) Field {
	return Field{Key: key, Value: value}
}

// Err creates the conventional `error` field.
func Err(err error) Field {
	return Field{Key: "error", Value: err}
}
