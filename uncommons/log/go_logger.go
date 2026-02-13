package log

import (
	"fmt"
	"log"
	"strings"
)

// logControlCharReplacer escapes control characters that can be used for log injection (CWE-117).
// Newlines, carriage returns, and tabs in log messages can forge fake log entries,
// mislead incident response, or inject false audit trail entries.
var logControlCharReplacer = strings.NewReplacer(
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// sanitizeLogString escapes control characters in a single string value.
func sanitizeLogString(s string) string {
	return logControlCharReplacer.Replace(s)
}

// sanitizeLogArgs escapes control characters in all string-typed arguments.
// Non-string arguments are passed through unchanged.
func sanitizeLogArgs(args []any) []any {
	sanitized := make([]any, len(args))
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			sanitized[i] = sanitizeLogString(s)
		} else {
			sanitized[i] = arg
		}
	}

	return sanitized
}

// GoLogger is the Go built-in (log) implementation of Logger interface.
//
// All string arguments are sanitized to prevent log injection (CWE-117).
type GoLogger struct {
	fields                 []any
	Level                  LogLevel
	defaultMessageTemplate string
}

// IsLevelEnabled checks if the given level is enabled.
func (l *GoLogger) IsLevelEnabled(level LogLevel) bool {
	if l == nil {
		return false
	}

	return l.Level >= level
}

// Info implements Info Logger interface function.
func (l *GoLogger) Info(args ...any) {
	if l.IsLevelEnabled(InfoLevel) {
		log.Print(l.hydrateWithLevel(InfoLevel, args...))
	}
}

// Infof implements Infof Logger interface function.
func (l *GoLogger) Infof(format string, args ...any) {
	if l.IsLevelEnabled(InfoLevel) {
		log.Print(l.hydrateWithLevel(InfoLevel, fmt.Sprintf(sanitizeLogString(format), args...)))
	}
}

// Infoln implements Infoln Logger interface function.
func (l *GoLogger) Infoln(args ...any) {
	if l.IsLevelEnabled(InfoLevel) {
		log.Println(l.hydrateLineWithLevel(InfoLevel, args...)...)
	}
}

// Error implements Error Logger interface function.
func (l *GoLogger) Error(args ...any) {
	if l.IsLevelEnabled(ErrorLevel) {
		log.Print(l.hydrateWithLevel(ErrorLevel, args...))
	}
}

// Errorf implements Errorf Logger interface function.
func (l *GoLogger) Errorf(format string, args ...any) {
	if l.IsLevelEnabled(ErrorLevel) {
		log.Print(l.hydrateWithLevel(ErrorLevel, fmt.Sprintf(sanitizeLogString(format), args...)))
	}
}

// Errorln implements Errorln Logger interface function.
func (l *GoLogger) Errorln(args ...any) {
	if l.IsLevelEnabled(ErrorLevel) {
		log.Println(l.hydrateLineWithLevel(ErrorLevel, args...)...)
	}
}

// Warn implements Warn Logger interface function.
func (l *GoLogger) Warn(args ...any) {
	if l.IsLevelEnabled(WarnLevel) {
		log.Print(l.hydrateWithLevel(WarnLevel, args...))
	}
}

// Warnf implements Warnf Logger interface function.
func (l *GoLogger) Warnf(format string, args ...any) {
	if l.IsLevelEnabled(WarnLevel) {
		log.Print(l.hydrateWithLevel(WarnLevel, fmt.Sprintf(sanitizeLogString(format), args...)))
	}
}

// Warnln implements Warnln Logger interface function.
func (l *GoLogger) Warnln(args ...any) {
	if l.IsLevelEnabled(WarnLevel) {
		log.Println(l.hydrateLineWithLevel(WarnLevel, args...)...)
	}
}

// Debug implements Debug Logger interface function.
func (l *GoLogger) Debug(args ...any) {
	if l.IsLevelEnabled(DebugLevel) {
		log.Print(l.hydrateWithLevel(DebugLevel, args...))
	}
}

// Debugf implements Debugf Logger interface function.
func (l *GoLogger) Debugf(format string, args ...any) {
	if l.IsLevelEnabled(DebugLevel) {
		log.Print(l.hydrateWithLevel(DebugLevel, fmt.Sprintf(sanitizeLogString(format), args...)))
	}
}

// Debugln implements Debugln Logger interface function.
func (l *GoLogger) Debugln(args ...any) {
	if l.IsLevelEnabled(DebugLevel) {
		log.Println(l.hydrateLineWithLevel(DebugLevel, args...)...)
	}
}

// Fatal implements Fatal Logger interface function.
func (l *GoLogger) Fatal(args ...any) {
	if l.IsLevelEnabled(FatalLevel) {
		log.Fatal(l.hydrateWithLevel(FatalLevel, args...))
	}
}

// Fatalf implements Fatalf Logger interface function.
func (l *GoLogger) Fatalf(format string, args ...any) {
	if l.IsLevelEnabled(FatalLevel) {
		log.Fatal(l.hydrateWithLevel(FatalLevel, fmt.Sprintf(sanitizeLogString(format), args...)))
	}
}

// Fatalln implements Fatalln Logger interface function.
func (l *GoLogger) Fatalln(args ...any) {
	if l.IsLevelEnabled(FatalLevel) {
		log.Fatalln(l.hydrateLineWithLevel(FatalLevel, args...)...)
	}
}

// WithFields implements WithFields Logger interface function.
//
//nolint:ireturn
func (l *GoLogger) WithFields(fields ...any) Logger {
	if l == nil {
		return &GoLogger{}
	}

	newFields := make([]any, 0, len(l.fields)+len(fields))
	newFields = append(newFields, l.fields...)
	newFields = append(newFields, fields...)

	return &GoLogger{
		Level:                  l.Level,
		fields:                 newFields,
		defaultMessageTemplate: l.defaultMessageTemplate,
	}
}

func (l *GoLogger) WithDefaultMessageTemplate(message string) Logger {
	if l == nil {
		return &GoLogger{}
	}

	return &GoLogger{
		Level:                  l.Level,
		fields:                 l.fields,
		defaultMessageTemplate: message,
	}
}

func (l *GoLogger) hydrateWithLevel(level LogLevel, args ...any) string {
	message := fmt.Sprint(sanitizeLogArgs(args)...)

	if l == nil {
		return message
	}

	messageParts := make([]string, 0, 4)
	messageParts = append(messageParts, fmt.Sprintf("[%s]", level.String()))

	if l.defaultMessageTemplate != "" {
		messageParts = append(messageParts, l.defaultMessageTemplate)
	}

	if fields := l.hydrateFields(); fields != "" {
		messageParts = append(messageParts, fields)
	}

	messageParts = append(messageParts, message)

	return strings.Join(messageParts, " ")
}

func (l *GoLogger) hydrateLineWithLevel(level LogLevel, args ...any) []any {
	safe := sanitizeLogArgs(args)

	if l == nil {
		return append([]any{fmt.Sprintf("[%s]", level.String())}, safe...)
	}

	parts := make([]any, 0, 3+len(safe))
	parts = append(parts, fmt.Sprintf("[%s]", level.String()))

	if l.defaultMessageTemplate != "" {
		parts = append(parts, l.defaultMessageTemplate)
	}

	if fields := l.hydrateFields(); fields != "" {
		parts = append(parts, fields)
	}

	return append(parts, safe...)
}

func (l *GoLogger) hydrateFields() string {
	if len(l.fields) == 0 {
		return ""
	}

	parts := make([]string, 0, (len(l.fields)+1)/2)

	for i := 0; i < len(l.fields); i += 2 {
		if i+1 >= len(l.fields) {
			parts = append(parts, fmt.Sprint(l.fields[i]))
			continue
		}

		parts = append(parts, fmt.Sprintf("%v=%v", l.fields[i], l.fields[i+1]))
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
}

// Sync implements Sync Logger interface function.
//
//nolint:ireturn
func (l *GoLogger) Sync() error { return nil }
