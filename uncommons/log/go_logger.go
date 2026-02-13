package log

import (
	"context"
	"fmt"
	"log"
	"strings"
)

var logControlCharReplacer = strings.NewReplacer(
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
	"\x00", `\0`,
)

func sanitizeLogString(s string) string {
	return logControlCharReplacer.Replace(s)
}

// GoLogger is the stdlib logger implementation for Logger.
type GoLogger struct {
	Level  Level
	fields []Field
	groups []string
}

// Enabled reports whether the logger emits entries at the given level.
// On a nil receiver, Enabled returns false silently. Use NopLogger as the
// documented nil-safe alternative.
func (l *GoLogger) Enabled(level Level) bool {
	if l == nil {
		return false
	}

	return l.Level >= level
}

// Log writes a single log line if the level is enabled.
func (l *GoLogger) Log(_ context.Context, level Level, msg string, fields ...Field) {
	if !l.Enabled(level) {
		return
	}

	line := l.hydrateLine(level, msg, fields...)
	log.Print(line)
}

//nolint:ireturn
func (l *GoLogger) With(fields ...Field) Logger {
	if l == nil {
		return &NopLogger{}
	}

	newFields := make([]Field, 0, len(l.fields)+len(fields))
	newFields = append(newFields, l.fields...)
	newFields = append(newFields, fields...)

	newGroups := make([]string, 0, len(l.groups))
	newGroups = append(newGroups, l.groups...)

	return &GoLogger{
		Level:  l.Level,
		fields: newFields,
		groups: newGroups,
	}
}

//nolint:ireturn
func (l *GoLogger) WithGroup(name string) Logger {
	if l == nil {
		return &NopLogger{}
	}

	newGroups := make([]string, 0, len(l.groups)+1)

	newGroups = append(newGroups, l.groups...)
	if strings.TrimSpace(name) != "" {
		newGroups = append(newGroups, sanitizeLogString(name))
	}

	newFields := make([]Field, 0, len(l.fields))
	newFields = append(newFields, l.fields...)

	return &GoLogger{
		Level:  l.Level,
		fields: newFields,
		groups: newGroups,
	}
}

// Sync flushes buffered logs. It is a no-op for the stdlib logger.
func (l *GoLogger) Sync(_ context.Context) error { return nil }

func (l *GoLogger) hydrateLine(level Level, msg string, fields ...Field) string {
	parts := make([]string, 0, 4)
	parts = append(parts, fmt.Sprintf("[%s]", level.String()))

	if l != nil && len(l.groups) > 0 {
		parts = append(parts, fmt.Sprintf("[group=%s]", strings.Join(l.groups, ".")))
	}

	allFields := make([]Field, 0, len(fields))
	if l != nil {
		allFields = append(allFields, l.fields...)
	}

	allFields = append(allFields, fields...)

	if rendered := renderFields(allFields); rendered != "" {
		parts = append(parts, rendered)
	}

	parts = append(parts, sanitizeLogString(msg))

	return strings.Join(parts, " ")
}

func renderFields(fields []Field) string {
	if len(fields) == 0 {
		return ""
	}

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		key := sanitizeLogString(field.Key)
		if key == "" {
			continue
		}

		parts = append(parts, fmt.Sprintf("%s=%v", key, sanitizeFieldValue(field.Value)))
	}

	if len(parts) == 0 {
		return ""
	}

	return fmt.Sprintf("[%s]", strings.Join(parts, ", "))
}

func sanitizeFieldValue(value any) any {
	s, ok := value.(string)
	if !ok {
		return value
	}

	return sanitizeLogString(s)
}
