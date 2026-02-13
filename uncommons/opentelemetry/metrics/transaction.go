package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

// RecordTransactionProcessed increments the transaction-processed counter.
func (f *MetricsFactory) RecordTransactionProcessed(ctx context.Context, attributes ...attribute.KeyValue) error {
	b, err := f.Counter(MetricTransactionsProcessed)
	if err != nil {
		return err
	}

	return b.WithAttributes(attributes...).AddOne(ctx)
}
