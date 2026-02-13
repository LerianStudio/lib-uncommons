package metrics

import (
	"context"
	"sync"
	"testing"

	"github.com/LerianStudio/lib-uncommons/uncommons/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestFactory creates a MetricsFactory wired to an in-memory ManualReader so
// we can collect and inspect metric data without any exporter.
func newTestFactory(t *testing.T) (*MetricsFactory, *sdkmetric.ManualReader) {
	t.Helper()

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := mp.Meter("test-lib")
	factory := NewMetricsFactory(meter, &log.NoneLogger{})

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	return factory, reader
}

// collectMetrics drains the ManualReader into a ResourceMetrics snapshot.
func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()

	var rm metricdata.ResourceMetrics

	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)

	return rm
}

// findMetric searches a ResourceMetrics snapshot for a metric by name.
func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}

	return nil
}

// sumCounterValue extracts the total monotonic sum from a counter metric.
func sumCounterValue(t *testing.T, m *metricdata.Metrics) int64 {
	t.Helper()

	data, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected Sum[int64] data type, got %T", m.Data)

	var total int64
	for _, dp := range data.DataPoints {
		total += dp.Value
	}

	return total
}

// counterDataPoints returns all data points for a counter metric.
func counterDataPoints(t *testing.T, m *metricdata.Metrics) []metricdata.DataPoint[int64] {
	t.Helper()

	data, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "expected Sum[int64] data type, got %T", m.Data)

	return data.DataPoints
}

// gaugeDataPoints returns all data points for a gauge metric.
func gaugeDataPoints(t *testing.T, m *metricdata.Metrics) []metricdata.DataPoint[int64] {
	t.Helper()

	data, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "expected Gauge[int64] data type, got %T", m.Data)

	return data.DataPoints
}

// histogramDataPoints returns all data points for a histogram metric.
func histogramDataPoints(t *testing.T, m *metricdata.Metrics) []metricdata.HistogramDataPoint[int64] {
	t.Helper()

	data, ok := m.Data.(metricdata.Histogram[int64])
	require.True(t, ok, "expected Histogram[int64] data type, got %T", m.Data)

	return data.DataPoints
}

// hasAttribute checks whether a set of KeyValues contains the given key=value pair.
func hasAttribute(attrs attribute.Set, key, value string) bool {
	v, found := attrs.Value(attribute.Key(key))
	if !found {
		return false
	}

	return v.AsString() == value
}

// ---------------------------------------------------------------------------
// 1. TestNewMetricsFactory
// ---------------------------------------------------------------------------

func TestNewMetricsFactory(t *testing.T) {
	factory, _ := newTestFactory(t)

	assert.NotNil(t, factory)
	assert.NotNil(t, factory.meter)
	assert.NotNil(t, factory.logger)
}

// ---------------------------------------------------------------------------
// 2. TestCounterBuilder
// ---------------------------------------------------------------------------

func TestCounterBuilder(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "test_counter", Description: "a test counter", Unit: "1"}

	// Add explicit value
	factory.Counter(m).Add(ctx, 5)
	// AddOne
	factory.Counter(m).AddOne(ctx)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "test_counter")
	require.NotNil(t, found, "metric test_counter not found in collected data")

	total := sumCounterValue(t, found)
	assert.Equal(t, int64(6), total)
}

// ---------------------------------------------------------------------------
// 3. TestCounterBuilder_WithLabels
// ---------------------------------------------------------------------------

func TestCounterBuilder_WithLabels(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "labeled_counter", Unit: "1"}
	labels := map[string]string{"env": "test", "region": "us-east-1"}

	factory.Counter(m).WithLabels(labels).AddOne(ctx)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "labeled_counter")
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	attrSet := dps[0].Attributes
	assert.True(t, hasAttribute(attrSet, "env", "test"), "expected attribute env=test")
	assert.True(t, hasAttribute(attrSet, "region", "us-east-1"), "expected attribute region=us-east-1")
}

// ---------------------------------------------------------------------------
// 4. TestCounterBuilder_WithAttributes
// ---------------------------------------------------------------------------

func TestCounterBuilder_WithAttributes(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "attr_counter", Unit: "1"}

	factory.Counter(m).WithAttributes(
		attribute.String("service", "ledger"),
		attribute.Int("version", 2),
	).AddOne(ctx)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "attr_counter")
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	attrSet := dps[0].Attributes
	assert.True(t, hasAttribute(attrSet, "service", "ledger"), "expected attribute service=ledger")

	v, ok := attrSet.Value(attribute.Key("version"))
	assert.True(t, ok, "expected attribute key 'version'")
	assert.Equal(t, int64(2), v.AsInt64())
}

// ---------------------------------------------------------------------------
// 5. TestCounterBuilder_NilCounter
// ---------------------------------------------------------------------------

func TestCounterBuilder_NilCounter(t *testing.T) {
	// A builder with a nil counter should no-op gracefully (no panic).
	builder := &CounterBuilder{
		factory: nil,
		counter: nil,
		name:    "nil_counter",
	}

	ctx := context.Background()

	assert.NotPanics(t, func() { builder.Add(ctx, 10) })
	assert.NotPanics(t, func() { builder.AddOne(ctx) })
}

// ---------------------------------------------------------------------------
// 6. TestGaugeBuilder_Set
// ---------------------------------------------------------------------------

func TestGaugeBuilder_Set(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "test_gauge", Description: "a test gauge", Unit: "1"}

	factory.Gauge(m).Set(ctx, 42)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "test_gauge")
	require.NotNil(t, found, "metric test_gauge not found")

	dps := gaugeDataPoints(t, found)
	require.NotEmpty(t, dps)
	assert.Equal(t, int64(42), dps[0].Value)
}

// ---------------------------------------------------------------------------
// 7. TestGaugeBuilder_Record_Deprecated
// ---------------------------------------------------------------------------

func TestGaugeBuilder_Record_Deprecated(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "deprecated_gauge", Unit: "1"}

	// Record is deprecated and delegates to Set.
	factory.Gauge(m).Record(ctx, 99)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "deprecated_gauge")
	require.NotNil(t, found)

	dps := gaugeDataPoints(t, found)
	require.NotEmpty(t, dps)
	assert.Equal(t, int64(99), dps[0].Value)
}

// ---------------------------------------------------------------------------
// 8. TestGaugeBuilder_NilGauge
// ---------------------------------------------------------------------------

func TestGaugeBuilder_NilGauge(t *testing.T) {
	builder := &GaugeBuilder{
		factory: nil,
		gauge:   nil,
		name:    "nil_gauge",
	}

	ctx := context.Background()

	assert.NotPanics(t, func() { builder.Set(ctx, 1) })
	assert.NotPanics(t, func() { builder.Record(ctx, 1) })
}

// ---------------------------------------------------------------------------
// 9. TestHistogramBuilder_Record
// ---------------------------------------------------------------------------

func TestHistogramBuilder_Record(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{
		Name:        "test_histogram",
		Description: "a test histogram",
		Unit:        "ms",
		Buckets:     []float64{10, 50, 100, 500, 1000},
	}

	factory.Histogram(m).Record(ctx, 42)
	factory.Histogram(m).Record(ctx, 99)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "test_histogram")
	require.NotNil(t, found, "metric test_histogram not found")

	dps := histogramDataPoints(t, found)
	require.NotEmpty(t, dps)

	assert.Equal(t, uint64(2), dps[0].Count, "expected 2 recorded values")
	assert.Equal(t, int64(42+99), dps[0].Sum, "expected sum of recorded values")
}

// ---------------------------------------------------------------------------
// 10. TestHistogramBuilder_WithBuckets
// ---------------------------------------------------------------------------

func TestHistogramBuilder_WithBuckets(t *testing.T) {
	// Buckets are configured via the Metric struct, not on the builder.
	// Verify that custom bucket boundaries are reflected in collected data.
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	customBuckets := []float64{1, 5, 10, 50}
	m := Metric{
		Name:    "custom_bucket_histogram",
		Unit:    "1",
		Buckets: customBuckets,
	}

	factory.Histogram(m).Record(ctx, 3)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "custom_bucket_histogram")
	require.NotNil(t, found)

	dps := histogramDataPoints(t, found)
	require.NotEmpty(t, dps)

	// The SDK returns explicit boundary + infinity bucket, so len(Bounds) == len(customBuckets).
	assert.Equal(t, customBuckets, dps[0].Bounds, "expected custom bucket boundaries")
}

// ---------------------------------------------------------------------------
// 11. TestHistogramBuilder_NilHistogram
// ---------------------------------------------------------------------------

func TestHistogramBuilder_NilHistogram(t *testing.T) {
	builder := &HistogramBuilder{
		factory:   nil,
		histogram: nil,
		name:      "nil_histogram",
	}

	ctx := context.Background()

	assert.NotPanics(t, func() { builder.Record(ctx, 100) })
}

// ---------------------------------------------------------------------------
// 12. TestSelectDefaultBuckets
// ---------------------------------------------------------------------------

func TestSelectDefaultBuckets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []float64
	}{
		{
			name:     "account pattern matches DefaultAccountBuckets",
			input:    "account_created",
			expected: DefaultAccountBuckets,
		},
		{
			name:     "transaction pattern matches DefaultTransactionBuckets",
			input:    "transactions_processed",
			expected: DefaultTransactionBuckets,
		},
		{
			name:     "latency pattern matches DefaultLatencyBuckets",
			input:    "request_latency",
			expected: DefaultLatencyBuckets,
		},
		{
			name:     "duration pattern matches DefaultLatencyBuckets",
			input:    "process_duration",
			expected: DefaultLatencyBuckets,
		},
		{
			name:     "time pattern matches DefaultLatencyBuckets",
			input:    "response_time",
			expected: DefaultLatencyBuckets,
		},
		{
			name:     "unknown metric falls back to DefaultLatencyBuckets",
			input:    "unknown_metric",
			expected: DefaultLatencyBuckets,
		},
		{
			name:     "case insensitive: ACCOUNT matches DefaultAccountBuckets",
			input:    "ACCOUNT_total",
			expected: DefaultAccountBuckets,
		},
		{
			name:     "priority: account takes precedence over transaction",
			input:    "account_transaction_mix",
			expected: DefaultAccountBuckets, // "account" is checked before "transaction"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectDefaultBuckets(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// 13. TestHistogramCacheKey
// ---------------------------------------------------------------------------

func TestHistogramCacheKey(t *testing.T) {
	tests := []struct {
		name     string
		metric   string
		buckets  []float64
		expected string
	}{
		{
			name:     "empty buckets returns just the name",
			metric:   "my_histogram",
			buckets:  nil,
			expected: "my_histogram",
		},
		{
			name:     "empty slice returns just the name",
			metric:   "my_histogram",
			buckets:  []float64{},
			expected: "my_histogram",
		},
		{
			name:     "buckets appended as sorted comma-separated",
			metric:   "request_latency",
			buckets:  []float64{1, 2, 3},
			expected: "request_latency:1,2,3",
		},
		{
			name:     "unsorted buckets are sorted in output",
			metric:   "mixed",
			buckets:  []float64{100, 10, 1, 50},
			expected: "mixed:1,10,50,100",
		},
		{
			name:     "float precision preserved",
			metric:   "precise",
			buckets:  []float64{0.001, 0.5, 2.5},
			expected: "precise:0.001,0.5,2.5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := histogramCacheKey(tt.metric, tt.buckets)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// ---------------------------------------------------------------------------
// 14. TestWithOrganizationLabels
// ---------------------------------------------------------------------------

func TestWithOrganizationLabels(t *testing.T) {
	factory, _ := newTestFactory(t)

	labels := factory.WithOrganizationLabels("org-123")

	assert.Equal(t, map[string]string{
		"organization_id": "org-123",
	}, labels)
}

// ---------------------------------------------------------------------------
// 15. TestWithLedgerLabels
// ---------------------------------------------------------------------------

func TestWithLedgerLabels(t *testing.T) {
	factory, _ := newTestFactory(t)

	labels := factory.WithLedgerLabels("org-456", "ledger-789")

	assert.Equal(t, map[string]string{
		"organization_id": "org-456",
		"ledger_id":       "ledger-789",
	}, labels)
}

// ---------------------------------------------------------------------------
// 16. TestConcurrentCounterCreation
// ---------------------------------------------------------------------------

func TestConcurrentCounterCreation(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	const goroutines = 100

	var wg sync.WaitGroup

	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			factory.Counter(MetricAccountsCreated).AddOne(ctx)
		}()
	}

	wg.Wait()

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricAccountsCreated.Name)
	require.NotNil(t, found, "metric %s not found after concurrent writes", MetricAccountsCreated.Name)

	total := sumCounterValue(t, found)
	assert.Equal(t, int64(goroutines), total, "expected total count to equal number of goroutines")
}

// ---------------------------------------------------------------------------
// 17. TestRecordAccountCreated
// ---------------------------------------------------------------------------

func TestRecordAccountCreated(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	factory.RecordAccountCreated(ctx, "org-a", "ledger-1")

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricAccountsCreated.Name)
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.Equal(t, int64(1), dps[0].Value)
	assert.True(t, hasAttribute(dps[0].Attributes, "organization_id", "org-a"))
	assert.True(t, hasAttribute(dps[0].Attributes, "ledger_id", "ledger-1"))
}

// ---------------------------------------------------------------------------
// 18. TestRecordTransactionProcessed
// ---------------------------------------------------------------------------

func TestRecordTransactionProcessed(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	factory.RecordTransactionProcessed(ctx, "org-b", "ledger-2")

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricTransactionsProcessed.Name)
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.Equal(t, int64(1), dps[0].Value)
	assert.True(t, hasAttribute(dps[0].Attributes, "organization_id", "org-b"))
	assert.True(t, hasAttribute(dps[0].Attributes, "ledger_id", "ledger-2"))
}

// ---------------------------------------------------------------------------
// 19. TestRecordOperationRouteCreated
// ---------------------------------------------------------------------------

func TestRecordOperationRouteCreated(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	factory.RecordOperationRouteCreated(ctx, "org-c", "ledger-3")

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricOperationRoutesCreated.Name)
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.Equal(t, int64(1), dps[0].Value)
	assert.True(t, hasAttribute(dps[0].Attributes, "organization_id", "org-c"))
	assert.True(t, hasAttribute(dps[0].Attributes, "ledger_id", "ledger-3"))
}

// ---------------------------------------------------------------------------
// 20. TestRecordTransactionRouteCreated
// ---------------------------------------------------------------------------

func TestRecordTransactionRouteCreated(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	factory.RecordTransactionRouteCreated(ctx, "org-d", "ledger-4")

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricTransactionRoutesCreated.Name)
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.Equal(t, int64(1), dps[0].Value)
	assert.True(t, hasAttribute(dps[0].Attributes, "organization_id", "org-d"))
	assert.True(t, hasAttribute(dps[0].Attributes, "ledger_id", "ledger-4"))
}

// ---------------------------------------------------------------------------
// 21. TestBuilderImmutability
// ---------------------------------------------------------------------------

func TestBuilderImmutability(t *testing.T) {
	factory, _ := newTestFactory(t)

	t.Run("CounterBuilder WithLabels returns new builder", func(t *testing.T) {
		original := factory.Counter(Metric{Name: "immut_counter", Unit: "1"})
		derived := original.WithLabels(map[string]string{"key": "val"})

		assert.NotSame(t, original, derived, "WithLabels must return a new CounterBuilder")
		assert.Empty(t, original.attrs, "original builder attrs must not be mutated")
		assert.NotEmpty(t, derived.attrs, "derived builder attrs must contain the new labels")
	})

	t.Run("CounterBuilder WithAttributes returns new builder", func(t *testing.T) {
		original := factory.Counter(Metric{Name: "immut_counter2", Unit: "1"})
		derived := original.WithAttributes(attribute.String("k", "v"))

		assert.NotSame(t, original, derived)
		assert.Empty(t, original.attrs)
		assert.NotEmpty(t, derived.attrs)
	})

	t.Run("GaugeBuilder WithLabels returns new builder", func(t *testing.T) {
		original := factory.Gauge(Metric{Name: "immut_gauge", Unit: "1"})
		derived := original.WithLabels(map[string]string{"key": "val"})

		assert.NotSame(t, original, derived, "WithLabels must return a new GaugeBuilder")
		assert.Empty(t, original.attrs)
		assert.NotEmpty(t, derived.attrs)
	})

	t.Run("GaugeBuilder WithAttributes returns new builder", func(t *testing.T) {
		original := factory.Gauge(Metric{Name: "immut_gauge2", Unit: "1"})
		derived := original.WithAttributes(attribute.String("k", "v"))

		assert.NotSame(t, original, derived)
		assert.Empty(t, original.attrs)
		assert.NotEmpty(t, derived.attrs)
	})

	t.Run("HistogramBuilder WithLabels returns new builder", func(t *testing.T) {
		original := factory.Histogram(Metric{Name: "immut_hist", Unit: "ms", Buckets: []float64{1, 10}})
		derived := original.WithLabels(map[string]string{"key": "val"})

		assert.NotSame(t, original, derived, "WithLabels must return a new HistogramBuilder")
		assert.Empty(t, original.attrs)
		assert.NotEmpty(t, derived.attrs)
	})

	t.Run("HistogramBuilder WithAttributes returns new builder", func(t *testing.T) {
		original := factory.Histogram(Metric{Name: "immut_hist2", Unit: "ms", Buckets: []float64{1, 10}})
		derived := original.WithAttributes(attribute.String("k", "v"))

		assert.NotSame(t, original, derived)
		assert.Empty(t, original.attrs)
		assert.NotEmpty(t, derived.attrs)
	})
}

// ---------------------------------------------------------------------------
// 22. TestAddCounterOptions / TestAddGaugeOptions / TestAddHistogramOptions
// ---------------------------------------------------------------------------

func TestAddCounterOptions(t *testing.T) {
	factory, _ := newTestFactory(t)

	tests := []struct {
		name        string
		metric      Metric
		expectedLen int
	}{
		{
			name:        "both description and unit produce 2 options",
			metric:      Metric{Name: "c1", Description: "desc", Unit: "1"},
			expectedLen: 2,
		},
		{
			name:        "description only produces 1 option",
			metric:      Metric{Name: "c2", Description: "desc"},
			expectedLen: 1,
		},
		{
			name:        "unit only produces 1 option",
			metric:      Metric{Name: "c3", Unit: "ms"},
			expectedLen: 1,
		},
		{
			name:        "empty metric produces 0 options",
			metric:      Metric{Name: "c4"},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := factory.addCounterOptions(tt.metric)
			assert.Len(t, opts, tt.expectedLen)
		})
	}
}

func TestAddGaugeOptions(t *testing.T) {
	factory, _ := newTestFactory(t)

	tests := []struct {
		name        string
		metric      Metric
		expectedLen int
	}{
		{
			name:        "both description and unit produce 2 options",
			metric:      Metric{Name: "g1", Description: "desc", Unit: "1"},
			expectedLen: 2,
		},
		{
			name:        "description only produces 1 option",
			metric:      Metric{Name: "g2", Description: "desc"},
			expectedLen: 1,
		},
		{
			name:        "unit only produces 1 option",
			metric:      Metric{Name: "g3", Unit: "ms"},
			expectedLen: 1,
		},
		{
			name:        "empty metric produces 0 options",
			metric:      Metric{Name: "g4"},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := factory.addGaugeOptions(tt.metric)
			assert.Len(t, opts, tt.expectedLen)
		})
	}
}

func TestAddHistogramOptions(t *testing.T) {
	factory, _ := newTestFactory(t)

	tests := []struct {
		name        string
		metric      Metric
		expectedLen int
	}{
		{
			name:        "description + unit + buckets produce 3 options",
			metric:      Metric{Name: "h1", Description: "desc", Unit: "ms", Buckets: []float64{1, 10}},
			expectedLen: 3,
		},
		{
			name:        "description + unit produce 2 options",
			metric:      Metric{Name: "h2", Description: "desc", Unit: "ms"},
			expectedLen: 2,
		},
		{
			name:        "buckets only produces 1 option",
			metric:      Metric{Name: "h3", Buckets: []float64{5, 10}},
			expectedLen: 1,
		},
		{
			name:        "empty metric produces 0 options",
			metric:      Metric{Name: "h4"},
			expectedLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := factory.addHistogramOptions(tt.metric)
			assert.Len(t, opts, tt.expectedLen)
		})
	}
}

// ---------------------------------------------------------------------------
// Additional edge-case coverage
// ---------------------------------------------------------------------------

// TestCounterCaching verifies that calling Counter() twice with the same Metric
// returns a builder backed by the exact same underlying counter (sync.Map cache hit).
func TestCounterCaching(t *testing.T) {
	factory, _ := newTestFactory(t)

	m := Metric{Name: "cached_counter", Unit: "1"}

	b1 := factory.Counter(m)
	b2 := factory.Counter(m)

	// Both builders must reference the same underlying counter instrument.
	assert.Same(t, b1.counter, b2.counter, "expected same underlying counter from cache")
}

// TestGaugeCaching verifies sync.Map caching for gauges.
func TestGaugeCaching(t *testing.T) {
	factory, _ := newTestFactory(t)

	m := Metric{Name: "cached_gauge", Unit: "1"}

	b1 := factory.Gauge(m)
	b2 := factory.Gauge(m)

	assert.Same(t, b1.gauge, b2.gauge, "expected same underlying gauge from cache")
}

// TestHistogramCaching verifies sync.Map caching for histograms with identical bucket config.
func TestHistogramCaching(t *testing.T) {
	factory, _ := newTestFactory(t)

	m := Metric{Name: "cached_histogram", Unit: "ms", Buckets: []float64{1, 5, 10}}

	b1 := factory.Histogram(m)
	b2 := factory.Histogram(m)

	assert.Same(t, b1.histogram, b2.histogram, "expected same underlying histogram from cache")
}

// TestHistogramDefaultBucketSelection verifies that Histogram() assigns default
// buckets when the Metric has none.
func TestHistogramDefaultBucketSelection(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	// "account_events" contains "account" → should get DefaultAccountBuckets
	m := Metric{Name: "account_events", Unit: "1"}

	factory.Histogram(m).Record(ctx, 7)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "account_events")
	require.NotNil(t, found)

	dps := histogramDataPoints(t, found)
	require.NotEmpty(t, dps)

	assert.Equal(t, DefaultAccountBuckets, dps[0].Bounds,
		"expected default account buckets to be applied automatically")
}

// TestBusinessConvenienceMethodsWithExtraAttributes verifies that the variadic
// attributes parameter on business convenience methods is propagated.
func TestBusinessConvenienceMethodsWithExtraAttributes(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	extra := attribute.String("account_type", "savings")
	factory.RecordAccountCreated(ctx, "org-x", "ledger-x", extra)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, MetricAccountsCreated.Name)
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.True(t, hasAttribute(dps[0].Attributes, "account_type", "savings"),
		"variadic attribute should be present in collected data")
	assert.True(t, hasAttribute(dps[0].Attributes, "organization_id", "org-x"))
	assert.True(t, hasAttribute(dps[0].Attributes, "ledger_id", "ledger-x"))
}

// TestPreConfiguredMetricDefinitions validates that the module-level Metric vars
// are defined with the expected names, descriptions and units.
func TestPreConfiguredMetricDefinitions(t *testing.T) {
	tests := []struct {
		metric      Metric
		wantName    string
		wantUnit    string
		wantDescNon bool // true → Description must be non-empty
	}{
		{MetricAccountsCreated, "accounts_created", "1", true},
		{MetricTransactionsProcessed, "transactions_processed", "1", true},
		{MetricTransactionRoutesCreated, "transaction_routes_created", "1", true},
		{MetricOperationRoutesCreated, "operation_routes_created", "1", true},
	}

	for _, tt := range tests {
		t.Run(tt.wantName, func(t *testing.T) {
			assert.Equal(t, tt.wantName, tt.metric.Name)
			assert.Equal(t, tt.wantUnit, tt.metric.Unit)

			if tt.wantDescNon {
				assert.NotEmpty(t, tt.metric.Description)
			}
		})
	}
}

// TestConcurrentGaugeAndHistogram exercises concurrent access on gauges and
// histograms to verify thread safety across all sync.Map-backed instrument types.
func TestConcurrentGaugeAndHistogram(t *testing.T) {
	factory, _ := newTestFactory(t)
	ctx := context.Background()

	const goroutines = 50

	var wg sync.WaitGroup

	wg.Add(goroutines * 2)

	gaugeMetric := Metric{Name: "conc_gauge", Unit: "1"}
	histMetric := Metric{Name: "conc_hist", Unit: "ms", Buckets: []float64{1, 10, 100}}

	for i := 0; i < goroutines; i++ {
		go func(v int) {
			defer wg.Done()
			factory.Gauge(gaugeMetric).Set(ctx, int64(v))
		}(i)

		go func(v int) {
			defer wg.Done()
			factory.Histogram(histMetric).Record(ctx, int64(v))
		}(i)
	}

	wg.Wait()
	// If we reach here without a panic or race, the test passes.
}

// ---------------------------------------------------------------------------
// Type assertion failure paths
// ---------------------------------------------------------------------------

func TestGetOrCreateCounter_TypeAssertionFailure(t *testing.T) {
	factory, _ := newTestFactory(t)
	ctx := context.Background()

	// Inject a wrong type into the counters sync.Map
	factory.counters.Store("corrupted_counter", "not-a-counter")

	m := Metric{Name: "corrupted_counter", Unit: "1"}

	// Counter() should not panic — getOrCreateCounter returns nil on type assertion failure
	builder := factory.Counter(m)
	assert.NotPanics(t, func() {
		builder.AddOne(ctx) // Should no-op gracefully via Add's nil guard
	})
}

func TestGetOrCreateGauge_TypeAssertionFailure(t *testing.T) {
	factory, _ := newTestFactory(t)
	ctx := context.Background()

	factory.gauges.Store("corrupted_gauge", 42) // wrong type

	m := Metric{Name: "corrupted_gauge", Unit: "1"}
	builder := factory.Gauge(m)

	assert.NotPanics(t, func() {
		builder.Set(ctx, 100) // Should no-op gracefully
	})
}

func TestGetOrCreateHistogram_TypeAssertionFailure(t *testing.T) {
	factory, _ := newTestFactory(t)
	ctx := context.Background()

	// histogramCacheKey generates the key — for no buckets, it's just the name
	factory.histograms.Store("corrupted_histogram", true) // wrong type

	m := Metric{Name: "corrupted_histogram", Unit: "ms"}
	builder := factory.Histogram(m)

	assert.NotPanics(t, func() {
		builder.Record(ctx, 50) // Should no-op gracefully
	})
}

// ---------------------------------------------------------------------------
// Chained labels and attributes
// ---------------------------------------------------------------------------

// TestChainedLabelsAndAttributes verifies that WithLabels followed by
// WithAttributes accumulates all attributes on the final builder.
func TestChainedLabelsAndAttributes(t *testing.T) {
	factory, reader := newTestFactory(t)
	ctx := context.Background()

	m := Metric{Name: "chained_counter", Unit: "1"}

	factory.Counter(m).
		WithLabels(map[string]string{"org": "lerian"}).
		WithAttributes(attribute.String("env", "prod")).
		AddOne(ctx)

	rm := collectMetrics(t, reader)
	found := findMetric(rm, "chained_counter")
	require.NotNil(t, found)

	dps := counterDataPoints(t, found)
	require.Len(t, dps, 1)

	assert.True(t, hasAttribute(dps[0].Attributes, "org", "lerian"))
	assert.True(t, hasAttribute(dps[0].Attributes, "env", "prod"))
}
