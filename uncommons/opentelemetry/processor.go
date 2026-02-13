package opentelemetry

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/LerianStudio/lib-uncommons/uncommons"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ---- SpanProcessor that applies the AttrBag to every new span ----

// AttrBagSpanProcessor copies request-scoped attributes from context into every span at start.
type AttrBagSpanProcessor struct{}

func (AttrBagSpanProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if kv := uncommons.AttributesFromContext(ctx); len(kv) > 0 {
		s.SetAttributes(kv...)
	}
}

func (AttrBagSpanProcessor) OnEnd(sdktrace.ReadOnlySpan) {}

func (AttrBagSpanProcessor) Shutdown(context.Context) error { return nil }

func (AttrBagSpanProcessor) ForceFlush(context.Context) error { return nil }

// ---- SpanProcessor that obfuscates sensitive attribute values on span end ----

// ObfuscatingSpanProcessor intercepts completed spans and replaces sensitive field values
// in string attributes with the obfuscator's redacted value. This is a pipeline-level
// approach: callers can use SetSpanAttributesFromStruct freely and obfuscation happens
// automatically before export, eliminating the need for per-call obfuscation functions.
//
// Usage:
//
//	tp := sdktrace.NewTracerProvider(
//	    sdktrace.WithSpanProcessor(opentelemetry.NewObfuscatingSpanProcessor(nil, nil)),
//	    // ... other options
//	)
//
// Pass nil for the default obfuscator (security.IsSensitiveField).
type ObfuscatingSpanProcessor struct {
	obfuscator FieldObfuscator
	next       sdktrace.SpanProcessor
}

// NewObfuscatingSpanProcessor creates a processor that obfuscates sensitive string
// attribute values. If obfuscator is nil, NewDefaultObfuscator is used.
// If next is nil, the processor operates standalone (no chaining).
func NewObfuscatingSpanProcessor(obfuscator FieldObfuscator, next sdktrace.SpanProcessor) *ObfuscatingSpanProcessor {
	if obfuscator == nil {
		obfuscator = NewDefaultObfuscator()
	}

	return &ObfuscatingSpanProcessor{
		obfuscator: obfuscator,
		next:       next,
	}
}

func (p *ObfuscatingSpanProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if p.next != nil {
		p.next.OnStart(ctx, s)
	}
}

// OnEnd walks all string attributes on the completed span. For each attribute whose
// key or JSON-decoded content contains sensitive field names, the value is replaced
// with the obfuscator's redacted placeholder.
func (p *ObfuscatingSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if s == nil {
		if p.next != nil {
			p.next.OnEnd(s)
		}

		return
	}

	// ReadOnlySpan doesn't allow mutation, so we use the ReadWriteSpan interface
	// if available. The SDK's *recordingSpan implements both.
	rw, ok := s.(sdktrace.ReadWriteSpan)
	if !ok {
		// Obfuscation cannot be applied — the span implementation does not
		// support mutation. This may indicate an OTel SDK change or a custom
		// span wrapper. Log once so operators notice if sensitive data is
		// passing through unredacted.
		log.Printf("opentelemetry: ObfuscatingSpanProcessor skipped — span %T does not implement ReadWriteSpan", s)

		if p.next != nil {
			p.next.OnEnd(s)
		}

		return
	}

	var redacted []attribute.KeyValue

	for _, attr := range s.Attributes() {
		if attr.Value.Type() != attribute.STRING {
			continue
		}

		key := string(attr.Key)
		val := attr.Value.AsString()

		newVal := p.obfuscateStringValue(key, val)
		if newVal != val {
			redacted = append(redacted, attribute.String(key, newVal))
		}
	}

	if len(redacted) > 0 {
		rw.SetAttributes(redacted...)
	}

	if p.next != nil {
		p.next.OnEnd(s)
	}
}

func (p *ObfuscatingSpanProcessor) Shutdown(ctx context.Context) error {
	if p.next != nil {
		return p.next.Shutdown(ctx)
	}

	return nil
}

func (p *ObfuscatingSpanProcessor) ForceFlush(ctx context.Context) error {
	if p.next != nil {
		return p.next.ForceFlush(ctx)
	}

	return nil
}

// obfuscateStringValue checks if the value is JSON (object or array) and walks it
// to obfuscate sensitive fields. If it's not JSON, it checks the key directly.
func (p *ObfuscatingSpanProcessor) obfuscateStringValue(key, val string) string {
	// Sanitize invalid UTF-8 first
	if !utf8.ValidString(val) {
		val = strings.ToValidUTF8(val, "\uFFFD")
	}

	// Guard against nil obfuscator (e.g., struct created without constructor)
	if p.obfuscator == nil {
		return val
	}

	// If the key itself is a sensitive field name, redact the entire value
	if p.obfuscator.ShouldObfuscate(key) {
		return p.obfuscator.GetObfuscatedValue()
	}

	// Try to parse as JSON and walk the structure
	trimmed := strings.TrimSpace(val)
	if len(trimmed) == 0 {
		return val
	}

	if trimmed[0] != '{' && trimmed[0] != '[' {
		return val
	}

	var data any
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return val
	}

	result := obfuscateStructFields(data, p.obfuscator)

	out, err := json.Marshal(result)
	if err != nil {
		return val
	}

	return string(out)
}
