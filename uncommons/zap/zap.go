package zap

import (
	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"go.uber.org/zap"
)

// ZapWithTraceLogger is a wrapper of zap.SugaredLogger with OpenTelemetry trace bridge.
//
// It implements the Logger interface from the log package.
type ZapWithTraceLogger struct {
	Logger                 *zap.SugaredLogger
	defaultMessageTemplate string
}

// logWithHydration is a helper method to log messages with hydrated arguments using the default message template.
// When no template is set, arguments are passed through unchanged to avoid prepending an empty string.
// All string arguments are sanitized to prevent log injection (CWE-117).
func (l *ZapWithTraceLogger) logWithHydration(logFunc func(...any), args ...any) {
	if l == nil {
		return
	}

	safe := sanitizeArgs(args)
	if l.defaultMessageTemplate != "" {
		logFunc(hydrateArgs(l.defaultMessageTemplate, safe)...)
	} else {
		logFunc(safe...)
	}
}

// logfWithHydration is a helper method to log formatted messages with hydrated arguments using the default message template.
// When no template is set, the format string is passed through unchanged.
// The format string is sanitized to prevent log injection (CWE-117).
func (l *ZapWithTraceLogger) logfWithHydration(logFunc func(string, ...any), format string, args ...any) {
	if l == nil {
		return
	}

	safeFormat := sanitizeFormat(format)
	safe := sanitizeArgs(args)

	if l.defaultMessageTemplate != "" {
		logFunc(l.defaultMessageTemplate+safeFormat, safe...)
	} else {
		logFunc(safeFormat, safe...)
	}
}

// Info implements Info Logger interface function.
func (l *ZapWithTraceLogger) Info(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Info, args...)
}

// Infof implements Infof Logger interface function.
func (l *ZapWithTraceLogger) Infof(format string, args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logfWithHydration(l.Logger.Infof, format, args...)
}

// Infoln implements Infoln Logger interface function.
func (l *ZapWithTraceLogger) Infoln(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Infoln, args...)
}

// Error implements Error Logger interface function.
func (l *ZapWithTraceLogger) Error(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Error, args...)
}

// Errorf implements Errorf Logger interface function.
func (l *ZapWithTraceLogger) Errorf(format string, args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logfWithHydration(l.Logger.Errorf, format, args...)
}

// Errorln implements Errorln Logger interface function.
func (l *ZapWithTraceLogger) Errorln(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Errorln, args...)
}

// Warn implements Warn Logger interface function.
func (l *ZapWithTraceLogger) Warn(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Warn, args...)
}

// Warnf implements Warnf Logger interface function.
func (l *ZapWithTraceLogger) Warnf(format string, args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logfWithHydration(l.Logger.Warnf, format, args...)
}

// Warnln implements Warnln Logger interface function.
func (l *ZapWithTraceLogger) Warnln(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Warnln, args...)
}

// Debug implements Debug Logger interface function.
func (l *ZapWithTraceLogger) Debug(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Debug, args...)
}

// Debugf implements Debugf Logger interface function.
func (l *ZapWithTraceLogger) Debugf(format string, args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logfWithHydration(l.Logger.Debugf, format, args...)
}

// Debugln implements Debugln Logger interface function.
func (l *ZapWithTraceLogger) Debugln(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Debugln, args...)
}

// Fatal implements Fatal Logger interface function.
func (l *ZapWithTraceLogger) Fatal(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Fatal, args...)
}

// Fatalf implements Fatalf Logger interface function.
func (l *ZapWithTraceLogger) Fatalf(format string, args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logfWithHydration(l.Logger.Fatalf, format, args...)
}

// Fatalln implements Fatalln Logger interface function.
func (l *ZapWithTraceLogger) Fatalln(args ...any) {
	if l == nil || l.Logger == nil {
		return
	}

	l.logWithHydration(l.Logger.Fatalln, args...)
}

// WithFields adds structured context to the logger. It returns a new logger and leaves the original unchanged.
//
//nolint:ireturn
func (l *ZapWithTraceLogger) WithFields(fields ...any) log.Logger {
	if l == nil || l.Logger == nil {
		return &ZapWithTraceLogger{}
	}

	return &ZapWithTraceLogger{
		Logger:                 l.Logger.With(fields...),
		defaultMessageTemplate: l.defaultMessageTemplate,
	}
}

// Sync implements Sync Logger interface function.
//
// Sync calls the underlying Core's Sync method, flushing any buffered log entries as well as
// closing the logger provider used by open telemetry. Applications should take care to call
// Sync before exiting.
func (l *ZapWithTraceLogger) Sync() error {
	if l == nil || l.Logger == nil {
		return nil
	}

	return l.Logger.Sync()
}

// WithDefaultMessageTemplate sets the default message template for the logger.
// Returns a new logger instance without mutating the original.
//
//nolint:ireturn
func (l *ZapWithTraceLogger) WithDefaultMessageTemplate(message string) log.Logger {
	if l == nil || l.Logger == nil {
		return &ZapWithTraceLogger{}
	}

	return &ZapWithTraceLogger{
		Logger:                 l.Logger,
		defaultMessageTemplate: message,
	}
}

// hydrateArgs prepends the default template message to the arguments slice.
func hydrateArgs(defaultTemplateMsg string, args []any) []any {
	return append([]any{defaultTemplateMsg}, args...)
}
