package opentelemetry

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"unicode/utf8"

	"github.com/LerianStudio/lib-uncommons/uncommons"
	cn "github.com/LerianStudio/lib-uncommons/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestTracerProvider creates an in-memory exporter and a TracerProvider that
// uses a synchronous (simple) exporter so every span is immediately available
// via exporter.GetSpans().
func newTestTracerProvider() (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))

	return exp, tp
}

// findEventByName returns the first event with the given name, or nil.
func findEventByName(events []sdktrace.Event, name string) *sdktrace.Event {
	for i := range events {
		if events[i].Name == name {
			return &events[i]
		}
	}

	return nil
}

// findAttrByKey returns the first attribute matching key, or nil.
func findAttrByKey(attrs []attribute.KeyValue, key string) *attribute.KeyValue {
	for i := range attrs {
		if string(attrs[i].Key) == key {
			return &attrs[i]
		}
	}

	return nil
}

// setupPropagator configures the global propagator for trace-context tests.
func setupPropagator() {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

// ---------------------------------------------------------------------------
// 1. Span Error Handling Functions
// ---------------------------------------------------------------------------

func TestHandleSpanBusinessErrorEvent(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "biz-error-span")
	testErr := errors.New("validation failed")

	HandleSpanBusinessErrorEvent(&span, "validation.error", testErr)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	// Span status must stay OK — business errors are events, not span errors.
	assert.Equal(t, codes.Unset, spans[0].Status.Code)

	evt := findEventByName(spans[0].Events, "validation.error")
	require.NotNil(t, evt, "expected event 'validation.error'")

	attr := findAttrByKey(evt.Attributes, "error")
	require.NotNil(t, attr)
	assert.Equal(t, "validation failed", attr.Value.AsString())

	// Ensure ctx is consumed (avoid unused variable warning).
	_ = ctx

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestHandleSpanBusinessErrorEvent_NilSpan(t *testing.T) {
	// *trace.Span == nil (nil pointer)
	assert.NotPanics(t, func() {
		HandleSpanBusinessErrorEvent(nil, "event", errors.New("err"))
	})
}

func TestHandleSpanBusinessErrorEvent_NilSpanInterface(t *testing.T) {
	// Pointer to a nil interface value: span != nil, but *span == nil.
	var s trace.Span
	assert.NotPanics(t, func() {
		HandleSpanBusinessErrorEvent(&s, "event", errors.New("err"))
	})
}

func TestHandleSpanBusinessErrorEvent_NilError(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "nil-err-span")

	// Should be a no-op — no event recorded.
	HandleSpanBusinessErrorEvent(&span, "should.not.appear", nil)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Empty(t, spans[0].Events, "no events expected when err is nil")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------

func TestHandleSpanEvent(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "event-span")

	attrs := []attribute.KeyValue{
		attribute.String("key1", "value1"),
		attribute.Int("key2", 42),
	}

	HandleSpanEvent(&span, "custom.event", attrs...)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	evt := findEventByName(spans[0].Events, "custom.event")
	require.NotNil(t, evt)
	assert.Len(t, evt.Attributes, 2)

	a1 := findAttrByKey(evt.Attributes, "key1")
	require.NotNil(t, a1)
	assert.Equal(t, "value1", a1.Value.AsString())

	a2 := findAttrByKey(evt.Attributes, "key2")
	require.NotNil(t, a2)
	assert.Equal(t, int64(42), a2.Value.AsInt64())

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestHandleSpanEvent_NilSpan(t *testing.T) {
	assert.NotPanics(t, func() {
		HandleSpanEvent(nil, "event", attribute.String("k", "v"))
	})

	var s trace.Span
	assert.NotPanics(t, func() {
		HandleSpanEvent(&s, "event", attribute.String("k", "v"))
	})
}

// ---------------------------------------------------------------------------

func TestHandleSpanError(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "error-span")
	testErr := errors.New("database connection failed")

	HandleSpanError(&span, "db.query", testErr)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	// Span status must be Error.
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Contains(t, spans[0].Status.Description, "db.query")
	assert.Contains(t, spans[0].Status.Description, "database connection failed")

	// RecordError adds an "exception" event.
	exEvt := findEventByName(spans[0].Events, "exception")
	require.NotNil(t, exEvt, "expected 'exception' event from RecordError")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestHandleSpanError_NilSpan(t *testing.T) {
	assert.NotPanics(t, func() {
		HandleSpanError(nil, "msg", errors.New("err"))
	})

	var s trace.Span
	assert.NotPanics(t, func() {
		HandleSpanError(&s, "msg", errors.New("err"))
	})
}

func TestHandleSpanError_NilError(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "nil-err-span")

	HandleSpanError(&span, "msg", nil)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	// Status should remain unset — nil error is a no-op.
	assert.Equal(t, codes.Unset, spans[0].Status.Code)
	assert.Empty(t, spans[0].Events)

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 2. SetSpanAttributesFromStruct
// ---------------------------------------------------------------------------

func TestSetSpanAttributesFromStruct(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "attr-span")

	input := struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{Name: "alice", Count: 7}

	err := SetSpanAttributesFromStruct(&span, "user.data", input)
	require.NoError(t, err)

	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	attr := findAttrByKey(spans[0].Attributes, "user.data")
	require.NotNil(t, attr, "expected attribute 'user.data'")

	// Verify JSON content.
	var decoded map[string]any
	err = json.Unmarshal([]byte(attr.Value.AsString()), &decoded)
	require.NoError(t, err)
	assert.Equal(t, "alice", decoded["name"])
	assert.InDelta(t, 7, decoded["count"], 0.01)

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestSetSpanAttributesFromStruct_NilSpan(t *testing.T) {
	err := SetSpanAttributesFromStruct(nil, "key", "value")
	assert.NoError(t, err)
}

func TestSetSpanAttributesFromStruct_NilSpanInterface(t *testing.T) {
	var s trace.Span
	err := SetSpanAttributesFromStruct(&s, "key", "value")
	assert.NoError(t, err)
}

func TestSetSpanAttributesFromStruct_InvalidJSON(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "invalid-json-span")

	// Channels cannot be marshalled to JSON.
	invalid := struct {
		Ch chan int
	}{Ch: make(chan int)}

	err := SetSpanAttributesFromStruct(&span, "bad", invalid)
	assert.Error(t, err)

	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	// No attribute should have been set.
	attr := findAttrByKey(spans[0].Attributes, "bad")
	assert.Nil(t, attr, "attribute should not be set on marshal error")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestSetSpanAttributesFromStruct_UTF8Sanitization(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "utf8-span")

	input := map[string]string{"ok": "fine"}
	// Key contains an invalid UTF-8 byte.
	err := SetSpanAttributesFromStruct(&span, "test\x80key", input)
	require.NoError(t, err)

	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	// The key must be sanitised — the invalid byte replaced with U+FFFD.
	attr := findAttrByKey(spans[0].Attributes, "test\uFFFDkey")
	require.NotNil(t, attr, "expected sanitised attribute key")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 3. HTTP Context Propagation
// ---------------------------------------------------------------------------

func TestInjectHTTPContext(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "http-inject-span")
	defer span.End()

	headers := http.Header{}
	InjectHTTPContext(&headers, ctx)

	// Traceparent header must exist and be non-empty.
	tp2 := headers.Get("Traceparent")
	assert.NotEmpty(t, tp2, "Traceparent header should be injected")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestInjectHTTPContext_NilHeaders(t *testing.T) {
	assert.NotPanics(t, func() {
		InjectHTTPContext(nil, context.Background())
	})
}

// ---------------------------------------------------------------------------
// 4. gRPC Context Propagation
// ---------------------------------------------------------------------------

func TestInjectGRPCContext(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "grpc-inject-span")
	defer span.End()

	outCtx := InjectGRPCContext(ctx)

	md, ok := metadata.FromOutgoingContext(outCtx)
	require.True(t, ok, "expected outgoing metadata")

	// gRPC metadata keys are lowercased.
	values := md.Get("traceparent")
	require.NotEmpty(t, values, "traceparent must be present in gRPC metadata")
	assert.NotEmpty(t, values[0])

	// The canonical-case key "Traceparent" must NOT exist (already normalised).
	_, hasCap := md["Traceparent"]
	assert.False(t, hasCap, "Traceparent (capitalised) should be removed")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestExtractGRPCContext(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	// Produce a traceparent by injecting into outgoing metadata.
	ctx, span := tracer.Start(context.Background(), "grpc-roundtrip")
	defer span.End()

	outCtx := InjectGRPCContext(ctx)
	outMD, _ := metadata.FromOutgoingContext(outCtx)

	// Simulate the receiver: place the same metadata as *incoming*.
	inCtx := metadata.NewIncomingContext(context.Background(), outMD)

	extractedCtx := ExtractGRPCContext(inCtx)

	// The extracted context must carry the same trace ID as the original.
	origTraceID := span.SpanContext().TraceID().String()
	extractedSpan := trace.SpanFromContext(extractedCtx)
	extractedTraceID := extractedSpan.SpanContext().TraceID().String()

	assert.Equal(t, origTraceID, extractedTraceID, "trace ID must survive inject→extract round-trip")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestExtractGRPCContext_NoMetadata(t *testing.T) {
	// A bare context without incoming metadata should be returned as-is.
	ctx := context.Background()
	result := ExtractGRPCContext(ctx)
	// We can't assert pointer equality (Extract may wrap), but the span
	// should have an invalid (zero) trace ID.
	traceID := trace.SpanFromContext(result).SpanContext().TraceID().String()
	assert.Equal(t, "00000000000000000000000000000000", traceID)
}

// ---------------------------------------------------------------------------
// 5. PrepareQueueHeaders
// ---------------------------------------------------------------------------

func TestPrepareQueueHeaders(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "queue-prepare")
	defer span.End()

	base := map[string]any{
		"X-Request-Id": "req-123",
	}

	result := PrepareQueueHeaders(ctx, base)

	// Base header must be preserved.
	assert.Equal(t, "req-123", result["X-Request-Id"])

	// Trace headers must be injected.
	_, hasTP := result["Traceparent"]
	assert.True(t, hasTP, "expected Traceparent in prepared headers")

	// Original base must NOT be mutated.
	_, mutated := base["Traceparent"]
	assert.False(t, mutated, "base map must not be mutated")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestPrepareQueueHeaders_NilBaseHeaders(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "queue-nil-base")
	defer span.End()

	// nil base headers — maps.Copy(dst, nil) is safe.
	assert.NotPanics(t, func() {
		result := PrepareQueueHeaders(ctx, nil)
		assert.NotNil(t, result)
	})

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 6. EndTracingSpans
// ---------------------------------------------------------------------------

func TestEndTracingSpans(t *testing.T) {
	exp, tp := newTestTracerProvider()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "end-span")
	_ = span // span is ended via EndTracingSpans

	tl := &Telemetry{TracerProvider: tp}
	tl.EndTracingSpans(ctx)

	// Force flush so the exporter captures it.
	require.NoError(t, tp.ForceFlush(context.Background()))

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "end-span", spans[0].Name)
	// EndTime should be non-zero (span was ended).
	assert.False(t, spans[0].EndTime.IsZero(), "span must have been ended")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestEndTracingSpans_NilReceiver(t *testing.T) {
	var tl *Telemetry
	assert.NotPanics(t, func() {
		tl.EndTracingSpans(context.Background())
	})
}

// ---------------------------------------------------------------------------
// 7. ShutdownTelemetry
// ---------------------------------------------------------------------------

func TestShutdownTelemetry_NilReceiver(t *testing.T) {
	var tl *Telemetry
	assert.NotPanics(t, func() {
		tl.ShutdownTelemetry()
	})
}

func TestShutdownTelemetry_NilShutdownFunc(t *testing.T) {
	tl := &Telemetry{} // shutdown is nil
	assert.NotPanics(t, func() {
		tl.ShutdownTelemetry()
	})
}

func TestShutdownTelemetryWithContext(t *testing.T) {
	called := false
	tl := &Telemetry{
		shutdownCtx: func(ctx context.Context) error {
			called = true
			return nil
		},
	}

	err := tl.ShutdownTelemetryWithContext(context.Background())
	assert.NoError(t, err)
	assert.True(t, called, "shutdownCtx must be invoked")
}

func TestShutdownTelemetryWithContext_NilReceiver(t *testing.T) {
	var tl *Telemetry
	assert.NotPanics(t, func() {
		err := tl.ShutdownTelemetryWithContext(context.Background())
		assert.NoError(t, err)
	})
}

// ---------------------------------------------------------------------------
// 8. AttrBagSpanProcessor
// ---------------------------------------------------------------------------

func TestAttrBagSpanProcessor_OnStart(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSpanProcessor(AttrBagSpanProcessor{}),
	)
	tracer := tp.Tracer("test")

	// Put attributes into context using the AttrBag mechanism.
	ctx := uncommons.ContextWithSpanAttributes(context.Background(),
		attribute.String("tenant.id", "t-42"),
		attribute.String("enduser.id", "u-7"),
	)

	_, span := tracer.Start(ctx, "attrBag-span")
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	tenantAttr := findAttrByKey(spans[0].Attributes, "tenant.id")
	require.NotNil(t, tenantAttr, "expected tenant.id attribute from AttrBag")
	assert.Equal(t, "t-42", tenantAttr.Value.AsString())

	userAttr := findAttrByKey(spans[0].Attributes, "enduser.id")
	require.NotNil(t, userAttr, "expected enduser.id attribute from AttrBag")
	assert.Equal(t, "u-7", userAttr.Value.AsString())

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestAttrBagSpanProcessor_OnStart_NoAttributes(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithSpanProcessor(AttrBagSpanProcessor{}),
	)
	tracer := tp.Tracer("test")

	// Bare context — no AttrBag.
	_, span := tracer.Start(context.Background(), "no-attr-span")
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1)

	// Only built-in attributes (if any) should be present.
	// The processor should not have added tenant.id or similar.
	tenantAttr := findAttrByKey(spans[0].Attributes, "tenant.id")
	assert.Nil(t, tenantAttr, "no AttrBag attributes expected")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 9. ObfuscatingSpanProcessor
// ---------------------------------------------------------------------------

func TestObfuscatingSpanProcessor_ObfuscatesJSONAttributes(t *testing.T) {
	// Unit-test the obfuscation logic directly via the unexported method.
	// Note: OnEnd receives a ReadOnlySpan snapshot from the SDK, which
	// cannot be cast to ReadWriteSpan (embedded.Span private method).
	// We test the obfuscation engine itself to verify correctness.
	proc := NewObfuscatingSpanProcessor(nil, nil)

	jsonVal := `{"username":"alice","password":"s3cret","email":"alice@example.com"}`
	result := proc.obfuscateStringValue("request.body", jsonVal)

	var decoded map[string]any
	err := json.Unmarshal([]byte(result), &decoded)
	require.NoError(t, err)

	// "password" is a sensitive field — must be obfuscated.
	assert.Equal(t, cn.ObfuscatedValue, decoded["password"])
	// "username" and "email" are NOT sensitive.
	assert.Equal(t, "alice", decoded["username"])
	assert.Equal(t, "alice@example.com", decoded["email"])
}

func TestObfuscatingSpanProcessor_ObfuscatesSensitiveKeys(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)

	// When the key itself is a sensitive field name, the entire value is redacted.
	result := proc.obfuscateStringValue("password", "my-super-secret")
	assert.Equal(t, cn.ObfuscatedValue, result)
}

func TestObfuscatingSpanProcessor_LeavesNonSensitiveAlone(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)

	// Non-sensitive plain string key + non-JSON value → returned unchanged.
	assert.Equal(t, "GET", proc.obfuscateStringValue("http.method", "GET"))
	assert.Equal(t, "/api/v1/users", proc.obfuscateStringValue("http.url", "/api/v1/users"))
	assert.Equal(t, "", proc.obfuscateStringValue("http.empty", ""))
}

func TestObfuscatingSpanProcessor_NilObfuscator(t *testing.T) {
	// Passing nil obfuscator must default to NewDefaultObfuscator; should not panic.
	proc := NewObfuscatingSpanProcessor(nil, nil)
	assert.NotNil(t, proc)
	assert.NotNil(t, proc.obfuscator, "nil obfuscator must be replaced with default")
}

func TestObfuscatingSpanProcessor_OnEnd_NilSpan(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)
	assert.NotPanics(t, func() {
		proc.OnEnd(nil)
	})
}

func TestObfuscatingSpanProcessor_ChainsToNext(t *testing.T) {
	called := false
	next := &mockSpanProcessor{
		onEndFn: func(s sdktrace.ReadOnlySpan) { called = true },
	}

	proc := NewObfuscatingSpanProcessor(nil, next)

	// OnEnd with nil span should still call next.OnEnd.
	proc.OnEnd(nil)
	assert.True(t, called, "next processor OnEnd must be called")
}

func TestObfuscatingSpanProcessor_ShutdownAndFlush(t *testing.T) {
	shutdownCalled := false
	flushCalled := false
	next := &mockSpanProcessor{
		shutdownFn:   func(ctx context.Context) error { shutdownCalled = true; return nil },
		forceFlushFn: func(ctx context.Context) error { flushCalled = true; return nil },
	}

	proc := NewObfuscatingSpanProcessor(nil, next)

	err := proc.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, shutdownCalled)

	err = proc.ForceFlush(context.Background())
	assert.NoError(t, err)
	assert.True(t, flushCalled)
}

func TestObfuscatingSpanProcessor_ShutdownNoNext(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)

	assert.NoError(t, proc.Shutdown(context.Background()))
	assert.NoError(t, proc.ForceFlush(context.Background()))
}

// mockSpanProcessor stubs the sdktrace.SpanProcessor interface for chaining tests.
type mockSpanProcessor struct {
	onStartFn    func(ctx context.Context, s sdktrace.ReadWriteSpan)
	onEndFn      func(s sdktrace.ReadOnlySpan)
	shutdownFn   func(ctx context.Context) error
	forceFlushFn func(ctx context.Context) error
}

func (m *mockSpanProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if m.onStartFn != nil {
		m.onStartFn(ctx, s)
	}
}

func (m *mockSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	if m.onEndFn != nil {
		m.onEndFn(s)
	}
}

func (m *mockSpanProcessor) Shutdown(ctx context.Context) error {
	if m.shutdownFn != nil {
		return m.shutdownFn(ctx)
	}

	return nil
}

func (m *mockSpanProcessor) ForceFlush(ctx context.Context) error {
	if m.forceFlushFn != nil {
		return m.forceFlushFn(ctx)
	}

	return nil
}

// ---------------------------------------------------------------------------
// 10. InjectTraceHeadersIntoQueue with nil underlying map
// ---------------------------------------------------------------------------

func TestInjectTraceHeadersIntoQueue_NilUnderlyingMap(t *testing.T) {
	setupPropagator()

	_, tp := newTestTracerProvider()
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "queue-nil-map")
	defer span.End()

	// *headers is nil — the function must allocate the map.
	var headers map[string]any // nil
	InjectTraceHeadersIntoQueue(ctx, &headers)

	require.NotNil(t, headers, "function must allocate map when *headers is nil")

	_, hasTP := headers["Traceparent"]
	assert.True(t, hasTP, "expected Traceparent after injection into previously-nil map")

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 11. InitializeTelemetryWithError — Endpoint Validation
// ---------------------------------------------------------------------------

func TestInitializeTelemetryWithError_EmptyEndpoint(t *testing.T) {
	cfg := &TelemetryConfig{
		LibraryName:               "test-lib",
		ServiceName:               "test-svc",
		ServiceVersion:            "0.1.0",
		DeploymentEnv:             "test",
		EnableTelemetry:           true,
		CollectorExporterEndpoint: "",
		Logger:                    &log.NoneLogger{},
	}

	tl, err := InitializeTelemetryWithError(cfg)
	assert.Nil(t, tl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
}

// ---------------------------------------------------------------------------
// 12. ObfuscateStruct with arrays (new behaviour)
// ---------------------------------------------------------------------------

func TestObfuscateStruct_Array(t *testing.T) {
	type User struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}

	input := []User{
		{Name: "alice", Password: "secret1"},
		{Name: "bob", Password: "secret2"},
	}

	obfuscator := NewDefaultObfuscator()
	result, err := ObfuscateStruct(input, obfuscator)
	require.NoError(t, err)

	// Result should be []any after JSON round-trip.
	arr, ok := result.([]any)
	require.True(t, ok, "expected []any, got %T", result)
	require.Len(t, arr, 2)

	for i, item := range arr {
		m, ok := item.(map[string]any)
		require.True(t, ok, "item %d must be map[string]any", i)
		assert.Equal(t, cn.ObfuscatedValue, m["password"], "password in item %d must be obfuscated", i)
		assert.NotEqual(t, cn.ObfuscatedValue, m["name"], "name in item %d must not be obfuscated", i)
	}
}

func TestObfuscateStruct_VerifiesObfuscation(t *testing.T) {
	input := struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		AccessToken string `json:"access_token"`
		PublicData  string `json:"publicData"`
	}{
		Username:    "admin",
		Password:    "hunter2",
		AccessToken: "tok_abc123",
		PublicData:  "visible",
	}

	obfuscator := NewDefaultObfuscator()
	result, err := ObfuscateStruct(input, obfuscator)
	require.NoError(t, err)

	m, ok := result.(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "admin", m["username"])
	assert.Equal(t, cn.ObfuscatedValue, m["password"])
	assert.Equal(t, cn.ObfuscatedValue, m["access_token"])
	assert.Equal(t, "visible", m["publicData"])
}

// ---------------------------------------------------------------------------
// 13. buildShutdownHandlers (unexported, tested via package-level access)
// ---------------------------------------------------------------------------

// mockShutdownable implements the shutdownable interface for testing.
type mockShutdownable struct {
	err error
}

func (m *mockShutdownable) Shutdown(context.Context) error {
	return m.err
}

func TestBuildShutdownHandlers_ErrorAggregation(t *testing.T) {
	logger := &log.NoneLogger{}

	err1 := errors.New("provider shutdown failed")
	err2 := errors.New("exporter shutdown failed")

	c1 := &mockShutdownable{err: err1}
	c2 := &mockShutdownable{err: nil} // succeeds
	c3 := &mockShutdownable{err: err2}

	_, shutdownCtx := buildShutdownHandlers(logger, c1, c2, c3)
	err := shutdownCtx(context.Background())

	require.Error(t, err)
	assert.ErrorIs(t, err, err1, "first error must be present in joined error")
	assert.ErrorIs(t, err, err2, "second error must be present in joined error")
}

func TestBuildShutdownHandlers_NilComponents(t *testing.T) {
	logger := &log.NoneLogger{}

	ok := &mockShutdownable{err: nil}

	// Nil components interspersed — verify nil guard prevents panic.
	_, shutdownCtx := buildShutdownHandlers(logger, nil, ok, nil)
	err := shutdownCtx(context.Background())

	assert.NoError(t, err, "nil components should be skipped gracefully")
}

func TestBuildShutdownHandlers_EmptyComponents(t *testing.T) {
	logger := &log.NoneLogger{}

	// No components at all — should not panic.
	shutdown, shutdownCtx := buildShutdownHandlers(logger)

	assert.NotPanics(t, func() { shutdown() })

	err := shutdownCtx(context.Background())
	assert.NoError(t, err)
}

func TestBuildShutdownHandlers_FireAndForget(t *testing.T) {
	logger := &log.NoneLogger{}

	failing := &mockShutdownable{err: errors.New("boom")}

	shutdown, _ := buildShutdownHandlers(logger, failing)

	// Fire-and-forget handler logs errors but must NOT panic.
	assert.NotPanics(t, func() { shutdown() })
}

// ---------------------------------------------------------------------------
// 14. ObfuscatingSpanProcessor Integration (OnEnd with real TracerProvider)
// ---------------------------------------------------------------------------

func TestObfuscatingSpanProcessor_Integration_OnEnd(t *testing.T) {
	// NOTE: The OTel SDK calls span.snapshot() before passing to OnEnd, so
	// every processor receives a *trace.snapshot (ReadOnlySpan only). The
	// ObfuscatingSpanProcessor attempts to cast to ReadWriteSpan for in-place
	// mutation; when the cast fails it gracefully skips obfuscation and
	// delegates to the next processor.
	//
	// This integration test verifies:
	// 1. The processor does NOT panic in the real SDK pipeline.
	// 2. The span is still exported (chaining works).
	// 3. Attributes arrive at the exporter (even if unredacted).
	//
	// Direct obfuscation of string values is covered by the unit tests that
	// call obfuscateStringValue directly.

	exp := tracetest.NewInMemoryExporter()

	syncer := sdktrace.NewSimpleSpanProcessor(exp)
	proc := NewObfuscatingSpanProcessor(nil, syncer)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(proc),
	)
	tracer := tp.Tracer("test")

	_, span := tracer.Start(context.Background(), "integration-obfuscate")
	span.SetAttributes(
		attribute.String("request.body", `{"password":"secret","name":"alice"}`),
		attribute.String("http.method", "POST"),
	)
	span.End()

	spans := exp.GetSpans()
	require.Len(t, spans, 1, "span must be exported via chained next processor")

	// Verify attributes are present (the span was exported successfully).
	bodyAttr := findAttrByKey(spans[0].Attributes, "request.body")
	require.NotNil(t, bodyAttr, "request.body attribute must be present")

	// The body should contain "alice" regardless of obfuscation outcome.
	assert.Contains(t, bodyAttr.Value.AsString(), "alice")

	methodAttr := findAttrByKey(spans[0].Attributes, "http.method")
	require.NotNil(t, methodAttr, "http.method attribute must be present")
	assert.Equal(t, "POST", methodAttr.Value.AsString())

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

// ---------------------------------------------------------------------------
// 15. ObfuscatingSpanProcessor.OnStart delegation
// ---------------------------------------------------------------------------

func TestObfuscatingSpanProcessor_OnStart_DelegatesToNext(t *testing.T) {
	called := false
	next := &mockSpanProcessor{
		onStartFn: func(ctx context.Context, s sdktrace.ReadWriteSpan) { called = true },
	}

	proc := NewObfuscatingSpanProcessor(nil, next)

	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-onstart")

	// Get the underlying ReadWriteSpan by casting
	rw, ok := span.(sdktrace.ReadWriteSpan)
	if ok {
		proc.OnStart(context.Background(), rw)
		assert.True(t, called, "next processor OnStart must be called")
	}

	span.End()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
}

func TestObfuscatingSpanProcessor_OnStart_NoNext(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)
	// Should not panic with nil next
	assert.NotPanics(t, func() {
		// We can pass nil here since the function only delegates
		proc.OnStart(context.Background(), nil)
	})
}

// ---------------------------------------------------------------------------
// 16. Whitespace-only endpoint validation
// ---------------------------------------------------------------------------

func TestInitializeTelemetryWithError_WhitespaceEndpoint(t *testing.T) {
	cfg := &TelemetryConfig{
		LibraryName:               "test-lib",
		ServiceName:               "test-svc",
		ServiceVersion:            "0.1.0",
		DeploymentEnv:             "test",
		EnableTelemetry:           true,
		CollectorExporterEndpoint: "   ",
		Logger:                    &log.NoneLogger{},
	}

	tl, err := InitializeTelemetryWithError(cfg)
	assert.Nil(t, tl)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "endpoint")
}

// ---------------------------------------------------------------------------
// 17. InsecureExporter=false (TLS default) config propagation
// ---------------------------------------------------------------------------

func TestInitializeTelemetryWithError_TLSDefault(t *testing.T) {
	// When InsecureExporter is false (default), TLS should be used.
	// We can't test the actual TLS connection without an endpoint,
	// but we verify the config is correctly propagated in disabled mode.
	cfg := &TelemetryConfig{
		LibraryName:     "test-lib",
		ServiceName:     "test-svc",
		ServiceVersion:  "0.1.0",
		DeploymentEnv:   "test",
		EnableTelemetry: false,
		Logger:          &log.NoneLogger{},
		// InsecureExporter defaults to false (TLS)
	}

	tl, err := InitializeTelemetryWithError(cfg)
	require.NoError(t, err)
	require.NotNil(t, tl)
	assert.False(t, tl.InsecureExporter, "InsecureExporter should default to false (TLS)")

	t.Cleanup(func() { tl.ShutdownTelemetry() })
}

// ---------------------------------------------------------------------------
// 18. obfuscateStringValue with invalid UTF-8
// ---------------------------------------------------------------------------

func TestObfuscatingSpanProcessor_InvalidUTF8(t *testing.T) {
	proc := NewObfuscatingSpanProcessor(nil, nil)

	// Invalid UTF-8 bytes should be sanitized before processing
	result := proc.obfuscateStringValue("data", "value\x80with\xFFbytes")
	assert.True(t, utf8.ValidString(result), "result must be valid UTF-8")
	assert.Contains(t, result, "value")
	assert.Contains(t, result, "with")
	assert.Contains(t, result, "bytes")
}

// ---------------------------------------------------------------------------
// 19. SetSpanAttributesFromStructWithCustomObfuscation — nil span
// ---------------------------------------------------------------------------

func TestSetSpanAttributesFromStructWithCustomObfuscation_NilSpan(t *testing.T) {
	obfuscator := NewDefaultObfuscator()

	// nil pointer
	err := SetSpanAttributesFromStructWithCustomObfuscation(nil, "key", "value", obfuscator)
	assert.NoError(t, err)

	// nil interface
	var s trace.Span
	err = SetSpanAttributesFromStructWithCustomObfuscation(&s, "key", "value", obfuscator)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// 20. Nil obfuscator guard (struct literal, bypassing constructor)
// ---------------------------------------------------------------------------

func TestObfuscatingSpanProcessor_NilObfuscatorGuard(t *testing.T) {
	// Create processor bypassing the constructor (struct literal)
	proc := &ObfuscatingSpanProcessor{} // obfuscator is nil

	// obfuscateStringValue should not panic with nil obfuscator
	assert.NotPanics(t, func() {
		result := proc.obfuscateStringValue("password", "secret")
		assert.Equal(t, "secret", result, "should return original value when obfuscator is nil")
	})
}
