package fas

import "context"

// MetricCollector represents a client for collecting metrics from an external source.
type MetricCollector interface {
	Name() string
	CollectMetric(ctx context.Context) (float64, error)
}
