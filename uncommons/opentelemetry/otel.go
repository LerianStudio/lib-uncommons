package opentelemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LerianStudio/lib-uncommons/v2/uncommons"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/assert"
	constant "github.com/LerianStudio/lib-uncommons/v2/uncommons/constants"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/log"
	"github.com/LerianStudio/lib-uncommons/v2/uncommons/opentelemetry/metrics"
	"github.com/gofiber/fiber/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.34.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/metadata"
)

const (
	maxSpanAttributeStringLength = 4096
	maxAttributeDepth            = 32
	maxAttributeCount            = 128
)

var (
	// ErrNilTelemetryLogger is returned when telemetry config has no logger.
	ErrNilTelemetryLogger = errors.New("telemetry config logger cannot be nil")
	// ErrEmptyEndpoint is returned when telemetry is enabled without exporter endpoint.
	ErrEmptyEndpoint = errors.New("collector exporter endpoint cannot be empty when telemetry is enabled")
	// ErrNilTelemetry is returned when a telemetry method receives a nil receiver.
	ErrNilTelemetry = errors.New("telemetry instance is nil")
	// ErrNilShutdown is returned when telemetry shutdown handlers are unavailable.
	ErrNilShutdown = errors.New("telemetry shutdown function is nil")
)

// TelemetryConfig configures tracing, metrics, logging, and propagation behavior.
type TelemetryConfig struct {
	LibraryName               string
	ServiceName               string
	ServiceVersion            string
	DeploymentEnv             string
	CollectorExporterEndpoint string
	EnableTelemetry           bool
	InsecureExporter          bool
	Logger                    log.Logger
	Propagator                propagation.TextMapPropagator
	Redactor                  *Redactor
}

// Telemetry holds configured OpenTelemetry providers and lifecycle handlers.
type Telemetry struct {
	TelemetryConfig
	TracerProvider *sdktrace.TracerProvider
	MeterProvider  *sdkmetric.MeterProvider
	LoggerProvider *sdklog.LoggerProvider
	MetricsFactory *metrics.MetricsFactory
	shutdown       func()
	shutdownCtx    func(context.Context) error
}

// NewTelemetry builds telemetry providers and exporters from configuration.
func NewTelemetry(cfg TelemetryConfig) (*Telemetry, error) {
	if cfg.Logger == nil {
		return nil, ErrNilTelemetryLogger
	}

	if cfg.Propagator == nil {
		cfg.Propagator = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	}

	if cfg.Redactor == nil {
		cfg.Redactor = NewDefaultRedactor()
	}

	if cfg.EnableTelemetry && strings.TrimSpace(cfg.CollectorExporterEndpoint) == "" {
		return nil, ErrEmptyEndpoint
	}

	ctx := context.Background()

	if !cfg.EnableTelemetry {
		cfg.Logger.Log(ctx, log.LevelWarn, "Telemetry disabled")

		mp := sdkmetric.NewMeterProvider()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(RedactingAttrBagSpanProcessor{Redactor: cfg.Redactor}))
		lp := sdklog.NewLoggerProvider()

		metricsFactory, err := metrics.NewMetricsFactory(mp.Meter(cfg.LibraryName), cfg.Logger)
		if err != nil {
			return nil, err
		}

		return &Telemetry{
			TelemetryConfig: cfg,
			TracerProvider:  tp,
			MeterProvider:   mp,
			LoggerProvider:  lp,
			MetricsFactory:  metricsFactory,
			shutdown:        func() {},
			shutdownCtx:     func(context.Context) error { return nil },
		}, nil
	}

	if cfg.InsecureExporter && cfg.DeploymentEnv != "" &&
		cfg.DeploymentEnv != "development" && cfg.DeploymentEnv != "local" {
		cfg.Logger.Log(ctx, log.LevelWarn,
			"InsecureExporter is enabled in non-development environment",
			log.String("environment", cfg.DeploymentEnv))
	}

	r := cfg.newResource()

	tExp, err := cfg.newTracerExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't initialize tracer exporter: %w", err)
	}

	mExp, err := cfg.newMetricExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't initialize metric exporter: %w", err)
	}

	lExp, err := cfg.newLoggerExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("can't initialize logger exporter: %w", err)
	}

	mp := cfg.newMeterProvider(r, mExp)
	tp := cfg.newTracerProvider(r, tExp)
	lp := cfg.newLoggerProvider(r, lExp)

	metricsFactory, err := metrics.NewMetricsFactory(mp.Meter(cfg.LibraryName), cfg.Logger)
	if err != nil {
		return nil, err
	}

	shutdown, shutdownCtx := buildShutdownHandlers(cfg.Logger, mp, tp, lp, tExp, mExp, lExp)

	return &Telemetry{
		TelemetryConfig: cfg,
		TracerProvider:  tp,
		MeterProvider:   mp,
		LoggerProvider:  lp,
		MetricsFactory:  metricsFactory,
		shutdown:        shutdown,
		shutdownCtx:     shutdownCtx,
	}, nil
}

// ApplyGlobals sets this instance as the process-global OTEL providers/propagator.
func (tl *Telemetry) ApplyGlobals() {
	if tl == nil {
		asserter := assert.New(context.Background(), nil, "opentelemetry", "ApplyGlobals")
		_ = asserter.NoError(context.Background(), ErrNilTelemetry, "cannot apply globals from nil telemetry instance")

		return
	}

	otel.SetTracerProvider(tl.TracerProvider)
	otel.SetMeterProvider(tl.MeterProvider)
	global.SetLoggerProvider(tl.LoggerProvider)
	otel.SetTextMapPropagator(tl.Propagator)
}

// Tracer returns a tracer from this telemetry instance.
func (tl *Telemetry) Tracer(name string) (trace.Tracer, error) {
	if tl == nil || tl.TracerProvider == nil {
		asserter := assert.New(context.Background(), nil, "opentelemetry", "Tracer")
		_ = asserter.NoError(context.Background(), ErrNilTelemetry, "telemetry tracer provider is nil")

		return nil, ErrNilTelemetry
	}

	return tl.TracerProvider.Tracer(name), nil
}

// Meter returns a meter from this telemetry instance.
func (tl *Telemetry) Meter(name string) (metric.Meter, error) {
	if tl == nil || tl.MeterProvider == nil {
		asserter := assert.New(context.Background(), nil, "opentelemetry", "Meter")
		_ = asserter.NoError(context.Background(), ErrNilTelemetry, "telemetry meter provider is nil")

		return nil, ErrNilTelemetry
	}

	return tl.MeterProvider.Meter(name), nil
}

// ShutdownTelemetry shuts down telemetry components using background context.
func (tl *Telemetry) ShutdownTelemetry() {
	if tl == nil {
		return
	}

	if err := tl.ShutdownTelemetryWithContext(context.Background()); err != nil {
		asserter := assert.New(context.Background(), tl.Logger, "opentelemetry", "ShutdownTelemetry")
		_ = asserter.NoError(context.Background(), err, "telemetry shutdown failed")

		return
	}
}

// ShutdownTelemetryWithContext shuts down telemetry components with caller context.
func (tl *Telemetry) ShutdownTelemetryWithContext(ctx context.Context) error {
	if tl == nil {
		asserter := assert.New(context.Background(), nil, "opentelemetry", "ShutdownTelemetryWithContext")
		_ = asserter.NoError(context.Background(), ErrNilTelemetry, "cannot shutdown nil telemetry")

		return ErrNilTelemetry
	}

	if tl.shutdownCtx != nil {
		return tl.shutdownCtx(ctx)
	}

	if tl.shutdown != nil {
		tl.shutdown()
		return nil
	}

	asserter := assert.New(context.Background(), tl.Logger, "opentelemetry", "ShutdownTelemetryWithContext")
	_ = asserter.NoError(context.Background(), ErrNilShutdown, "cannot shutdown telemetry without configured shutdown function")

	return ErrNilShutdown
}

func (tl *TelemetryConfig) newResource() *sdkresource.Resource {
	return sdkresource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(tl.ServiceName),
		semconv.ServiceVersion(tl.ServiceVersion),
		semconv.DeploymentEnvironmentName(tl.DeploymentEnv),
		semconv.TelemetrySDKName(constant.TelemetrySDKName),
		semconv.TelemetrySDKLanguageGo,
	)
}

func (tl *TelemetryConfig) newLoggerExporter(ctx context.Context) (*otlploggrpc.Exporter, error) {
	opts := []otlploggrpc.Option{otlploggrpc.WithEndpoint(tl.CollectorExporterEndpoint)}
	if tl.InsecureExporter {
		opts = append(opts, otlploggrpc.WithInsecure())
	}

	return otlploggrpc.New(ctx, opts...)
}

func (tl *TelemetryConfig) newMetricExporter(ctx context.Context) (*otlpmetricgrpc.Exporter, error) {
	opts := []otlpmetricgrpc.Option{otlpmetricgrpc.WithEndpoint(tl.CollectorExporterEndpoint)}
	if tl.InsecureExporter {
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	}

	return otlpmetricgrpc.New(ctx, opts...)
}

func (tl *TelemetryConfig) newTracerExporter(ctx context.Context) (*otlptrace.Exporter, error) {
	opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(tl.CollectorExporterEndpoint)}
	if tl.InsecureExporter {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	return otlptracegrpc.New(ctx, opts...)
}

func (tl *TelemetryConfig) newLoggerProvider(rsc *sdkresource.Resource, exp *otlploggrpc.Exporter) *sdklog.LoggerProvider {
	bp := sdklog.NewBatchProcessor(exp)
	return sdklog.NewLoggerProvider(sdklog.WithResource(rsc), sdklog.WithProcessor(bp))
}

func (tl *TelemetryConfig) newMeterProvider(res *sdkresource.Resource, exp *otlpmetricgrpc.Exporter) *sdkmetric.MeterProvider {
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
	)
}

func (tl *TelemetryConfig) newTracerProvider(rsc *sdkresource.Resource, exp *otlptrace.Exporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithResource(rsc),
		sdktrace.WithSpanProcessor(RedactingAttrBagSpanProcessor{Redactor: tl.Redactor}),
		sdktrace.WithBatcher(exp),
	)
}

type shutdownable interface {
	Shutdown(ctx context.Context) error
}

// isNilShutdownable checks for both untyped nil and interface-wrapped typed nil
// (e.g., a concrete pointer that is nil but stored in a shutdownable interface).
func isNilShutdownable(s shutdownable) bool {
	if s == nil {
		return true
	}

	v := reflect.ValueOf(s)

	return v.Kind() == reflect.Ptr && v.IsNil()
}

func buildShutdownHandlers(l log.Logger, components ...shutdownable) (func(), func(context.Context) error) {
	shutdown := func() {
		ctx := context.Background()

		for _, c := range components {
			if isNilShutdownable(c) {
				continue
			}

			if err := c.Shutdown(ctx); err != nil {
				l.Log(ctx, log.LevelError, fmt.Sprintf("telemetry shutdown error: %v", err))
			}
		}
	}

	shutdownCtx := func(ctx context.Context) error {
		var errs []error

		for _, c := range components {
			if isNilShutdownable(c) {
				continue
			}

			if err := c.Shutdown(ctx); err != nil {
				errs = append(errs, err)
			}
		}

		return errors.Join(errs...)
	}

	return shutdown, shutdownCtx
}

// HandleSpanBusinessErrorEvent records a business-error event on a span.
func HandleSpanBusinessErrorEvent(span trace.Span, eventName string, err error) {
	if span == nil || err == nil {
		return
	}

	span.AddEvent(eventName, trace.WithAttributes(attribute.String("error", err.Error())))
}

// HandleSpanEvent records a generic event with optional attributes on a span.
func HandleSpanEvent(span trace.Span, eventName string, attributes ...attribute.KeyValue) {
	if span == nil {
		return
	}

	span.AddEvent(eventName, trace.WithAttributes(attributes...))
}

// HandleSpanError marks a span as failed and records the error.
func HandleSpanError(span trace.Span, message string, err error) {
	if span == nil || err == nil {
		return
	}

	span.SetStatus(codes.Error, message+": "+err.Error())
	span.RecordError(err)
}

// SetSpanAttributesFromValue flattens a value and sets resulting attributes on a span.
func SetSpanAttributesFromValue(span trace.Span, prefix string, value any, redactor *Redactor) error {
	if span == nil {
		return nil
	}

	attrs, err := BuildAttributesFromValue(prefix, value, redactor)
	if err != nil {
		return err
	}

	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}

	return nil
}

// BuildAttributesFromValue flattens a value into OTEL attributes with optional redaction.
func BuildAttributesFromValue(prefix string, value any, redactor *Redactor) ([]attribute.KeyValue, error) {
	if value == nil {
		return nil, nil
	}

	processed := value

	if redactor != nil {
		var err error

		processed, err = ObfuscateStruct(value, redactor)
		if err != nil {
			return nil, err
		}
	}

	b, err := json.Marshal(processed)
	if err != nil {
		return nil, err
	}

	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return nil, err
	}

	attrs := make([]attribute.KeyValue, 0, 16)
	flattenAttributes(&attrs, sanitizeUTF8String(prefix), decoded, 0)

	return attrs, nil
}

func flattenAttributes(attrs *[]attribute.KeyValue, prefix string, value any, depth int) {
	if depth >= maxAttributeDepth {
		return
	}

	if len(*attrs) >= maxAttributeCount {
		return
	}

	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			next := sanitizeUTF8String(key)
			if prefix != "" {
				next = prefix + "." + next
			}

			flattenAttributes(attrs, next, child, depth+1)
		}
	case []any:
		for i, child := range v {
			next := prefix + "." + strconv.Itoa(i)
			flattenAttributes(attrs, next, child, depth+1)
		}
	case string:
		s := sanitizeUTF8String(v)
		if len(s) > maxSpanAttributeStringLength {
			s = s[:maxSpanAttributeStringLength]
		}

		*attrs = append(*attrs, attribute.String(prefix, s))
	case float64:
		*attrs = append(*attrs, attribute.Float64(prefix, v))
	case bool:
		*attrs = append(*attrs, attribute.Bool(prefix, v))
	case json.Number:
		*attrs = append(*attrs, attribute.String(prefix, string(v)))
	case nil:
		return
	default:
		*attrs = append(*attrs, attribute.String(prefix, sanitizeUTF8String(fmt.Sprint(v))))
	}
}

// SetSpanAttributeForParam adds a request parameter attribute to the current context bag.
func SetSpanAttributeForParam(c *fiber.Ctx, param, value, entityName string) {
	spanAttrKey := "app.request." + param
	if entityName != "" && param == "id" {
		spanAttrKey = "app.request." + entityName + "_id"
	}

	c.SetUserContext(uncommons.ContextWithSpanAttributes(c.UserContext(), attribute.String(spanAttrKey, value)))
}

// InjectTraceContext injects trace context into a generic text map carrier.
func InjectTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) {
	if carrier == nil {
		return
	}

	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractTraceContext extracts trace context from a generic text map carrier.
func ExtractTraceContext(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	if carrier == nil {
		return ctx
	}

	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// InjectHTTPContext injects trace headers into HTTP headers.
func InjectHTTPContext(ctx context.Context, headers http.Header) {
	if headers == nil {
		return
	}

	InjectTraceContext(ctx, propagation.HeaderCarrier(headers))
}

// ExtractHTTPContext extracts trace headers from a Fiber request.
func ExtractHTTPContext(ctx context.Context, c *fiber.Ctx) context.Context {
	if c == nil {
		return ctx
	}

	carrier := propagation.HeaderCarrier{}
	for key, value := range c.Request().Header.All() {
		carrier.Set(string(key), string(value))
	}

	return ExtractTraceContext(ctx, carrier)
}

// InjectGRPCContext injects trace context into gRPC metadata.
func InjectGRPCContext(ctx context.Context, md metadata.MD) metadata.MD {
	if md == nil {
		md = metadata.New(nil)
	}

	InjectTraceContext(ctx, propagation.HeaderCarrier(md))

	if traceparentValues, exists := md["Traceparent"]; exists && len(traceparentValues) > 0 {
		md[constant.MetadataTraceparent] = traceparentValues
		delete(md, "Traceparent")
	}

	if tracestateValues, exists := md["Tracestate"]; exists && len(tracestateValues) > 0 {
		md[constant.MetadataTracestate] = tracestateValues
		delete(md, "Tracestate")
	}

	return md
}

// ExtractGRPCContext extracts trace context from gRPC metadata.
func ExtractGRPCContext(ctx context.Context, md metadata.MD) context.Context {
	if md == nil {
		return ctx
	}

	mdCopy := md.Copy()

	if traceparentValues, exists := mdCopy[constant.MetadataTraceparent]; exists && len(traceparentValues) > 0 {
		mdCopy["Traceparent"] = traceparentValues
		delete(mdCopy, constant.MetadataTraceparent)
	}

	if tracestateValues, exists := mdCopy[constant.MetadataTracestate]; exists && len(tracestateValues) > 0 {
		mdCopy["Tracestate"] = tracestateValues
		delete(mdCopy, constant.MetadataTracestate)
	}

	return ExtractTraceContext(ctx, propagation.HeaderCarrier(mdCopy))
}

// InjectQueueTraceContext serializes trace context to string headers for queues.
func InjectQueueTraceContext(ctx context.Context) map[string]string {
	carrier := propagation.HeaderCarrier{}
	InjectTraceContext(ctx, carrier)

	headers := make(map[string]string, len(carrier))
	for k, v := range carrier {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return headers
}

// ExtractQueueTraceContext extracts trace context from queue string headers.
func ExtractQueueTraceContext(ctx context.Context, headers map[string]string) context.Context {
	if headers == nil {
		return ctx
	}

	carrier := propagation.HeaderCarrier{}
	for k, v := range headers {
		carrier.Set(k, v)
	}

	return ExtractTraceContext(ctx, carrier)
}

// PrepareQueueHeaders merges base headers with propagated trace headers.
func PrepareQueueHeaders(ctx context.Context, baseHeaders map[string]any) map[string]any {
	headers := make(map[string]any)
	maps.Copy(headers, baseHeaders)

	traceHeaders := InjectQueueTraceContext(ctx)
	for k, v := range traceHeaders {
		headers[k] = v
	}

	return headers
}

// InjectTraceHeadersIntoQueue injects propagated trace headers into a mutable map.
func InjectTraceHeadersIntoQueue(ctx context.Context, headers *map[string]any) {
	if headers == nil {
		return
	}

	if *headers == nil {
		*headers = make(map[string]any)
	}

	traceHeaders := InjectQueueTraceContext(ctx)
	for k, v := range traceHeaders {
		(*headers)[k] = v
	}
}

// ExtractTraceContextFromQueueHeaders extracts trace context from AMQP-style headers.
func ExtractTraceContextFromQueueHeaders(baseCtx context.Context, amqpHeaders map[string]any) context.Context {
	if len(amqpHeaders) == 0 {
		return baseCtx
	}

	traceHeaders := make(map[string]string)

	for k, v := range amqpHeaders {
		if str, ok := v.(string); ok {
			traceHeaders[k] = str
		}
	}

	if len(traceHeaders) == 0 {
		return baseCtx
	}

	return ExtractQueueTraceContext(baseCtx, traceHeaders)
}

// GetTraceIDFromContext returns the current span trace ID, or empty if unavailable.
func GetTraceIDFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)

	sc := span.SpanContext()
	if !sc.IsValid() {
		return ""
	}

	return sc.TraceID().String()
}

// GetTraceStateFromContext returns the current span tracestate, or empty if unavailable.
func GetTraceStateFromContext(ctx context.Context) string {
	span := trace.SpanFromContext(ctx)

	sc := span.SpanContext()
	if !sc.IsValid() {
		return ""
	}

	return sc.TraceState().String()
}

func sanitizeUTF8String(s string) string {
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "�")
	}

	return s
}
