package log

import (
	"fmt"
	"strings"
)

// Logger is the pkg interface for log implementation.
//
//go:generate mockgen --destination=log_mock.go --package=log . Logger
type Logger interface {
	Info(args ...any)
	Infof(format string, args ...any)
	Infoln(args ...any)

	Error(args ...any)
	Errorf(format string, args ...any)
	Errorln(args ...any)

	Warn(args ...any)
	Warnf(format string, args ...any)
	Warnln(args ...any)

	Debug(args ...any)
	Debugf(format string, args ...any)
	Debugln(args ...any)

	Fatal(args ...any)
	Fatalf(format string, args ...any)
	Fatalln(args ...any)

	WithFields(fields ...any) Logger

	WithDefaultMessageTemplate(message string) Logger

	Sync() error
}

// LogLevel represents the level of log system (fatal, error, warn, info and debug).
type LogLevel int8

// These are the different log levels. You can set the logging level to log.
const (
	// PanicLevel level, highest level of severity. Logs and then calls panic with the
	// message passed to Debug, Info, ...
	PanicLevel LogLevel = iota
	// FatalLevel level. Logs and then calls `logger.Exit(1)`. It will exit even if the
	// logging level is set to Panic.
	FatalLevel
	// ErrorLevel level. Logs. Used for errors that should definitely be noted.
	// Commonly used for hooks to send errors to an error tracking service.
	ErrorLevel
	// WarnLevel level. Non-critical entries that deserve eyes.
	WarnLevel
	// InfoLevel level. General operational entries about what's going on inside the
	// application.
	InfoLevel
	// DebugLevel level. Usually only enabled when debugging. Very verbose logging.
	DebugLevel
)

func (level LogLevel) String() string {
	switch level {
	case PanicLevel:
		return "panic"
	case FatalLevel:
		return "fatal"
	case ErrorLevel:
		return "error"
	case WarnLevel:
		return "warn"
	case InfoLevel:
		return "info"
	case DebugLevel:
		return "debug"
	default:
		return "unknown"
	}
}

// ParseLevel takes a string level and returns a LogLevel constant.
func ParseLevel(lvl string) (LogLevel, error) {
	switch strings.ToLower(lvl) {
	case "panic":
		return PanicLevel, nil
	case "fatal":
		return FatalLevel, nil
	case "error":
		return ErrorLevel, nil
	case "warn", "warning":
		return WarnLevel, nil
	case "info":
		return InfoLevel, nil
	case "debug":
		return DebugLevel, nil
	}

	var l LogLevel

	return l, fmt.Errorf("not a valid LogLevel: %q", lvl)
}
