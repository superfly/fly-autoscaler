package fas

import (
	"context"
	"os"
)

// MetricCollector represents a client for collecting metrics from an external source.
type MetricCollector interface {
	Name() string
	CollectMetric(ctx context.Context, app string) (float64, error)
}

// ExpandMetricQuery replaces variables in query with their values.
func ExpandMetricQuery(ctx context.Context, query, app string) string {
	return os.Expand(query, func(key string) string {
		switch key {
		case "APP_NAME":
			return app
		default:
			return os.Getenv(key)
		}
	})
}
