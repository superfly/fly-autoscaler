package mock

import (
	"context"

	fas "github.com/superfly/fly-autoscaler"
)

var _ fas.MetricCollector = (*MetricCollector)(nil)

type MetricCollector struct {
	name              string
	CollectMetricFunc func(ctx context.Context) (float64, error)
}

func NewMetricCollector(name string) *MetricCollector {
	return &MetricCollector{name: name}
}

func (c *MetricCollector) Name() string { return c.name }

func (c *MetricCollector) CollectMetric(ctx context.Context) (float64, error) {
	return c.CollectMetricFunc(ctx)
}
