package metrics

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
)

// RecordAccountCreated increments the account-created counter.
func (f *MetricsFactory) RecordAccountCreated(ctx context.Context, attributes ...attribute.KeyValue) error {
	b, err := f.Counter(MetricAccountsCreated)
	if err != nil {
		return err
	}

	return b.WithAttributes(attributes...).AddOne(ctx)
}
