package zap

import (
	"strings"
)

// controlCharReplacer escapes control characters that can be used for log injection (CWE-117).
// Newlines, carriage returns, and tabs in log messages can forge fake log entries in console encoders,
// mislead incident response, or inject false audit trail entries.
//
// The JSON encoder already escapes these inside string values, so this is primarily a defense
// for development/staging environments using the console encoder.
var controlCharReplacer = strings.NewReplacer(
	"\n", `\n`,
	"\r", `\r`,
	"\t", `\t`,
)

// sanitizeString escapes control characters in a single string value.
func sanitizeString(s string) string {
	return controlCharReplacer.Replace(s)
}

// sanitizeArgs escapes control characters in all string-typed arguments.
// Non-string arguments are passed through unchanged.
func sanitizeArgs(args []any) []any {
	sanitized := make([]any, len(args))
	for i, arg := range args {
		if s, ok := arg.(string); ok {
			sanitized[i] = sanitizeString(s)
		} else {
			sanitized[i] = arg
		}
	}

	return sanitized
}

// sanitizeFormat escapes control characters in a format string.
// This prevents format strings themselves from containing injected newlines.
// The format verbs (%s, %d, etc.) are preserved — only literal control characters are escaped.
//
// Note: values substituted via %s/%v are the caller's responsibility. The sanitizeArgs
// function handles string arguments separately, but non-string Stringer implementations
// that return newlines will pass through. This is an acceptable tradeoff: sanitizing
// arbitrary Stringer output would require calling String() eagerly, defeating lazy evaluation.
func sanitizeFormat(format string) string {
	return sanitizeString(format)
}
