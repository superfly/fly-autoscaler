package prometheus

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	fas "github.com/superfly/fly-autoscaler"
)

var _ fas.MetricCollector = (*MetricCollector)(nil)

type MetricCollector struct {
	name  string
	query string
	api   v1.API
}

func NewMetricCollector(name, address, query string) (*MetricCollector, error) {
	client, err := api.NewClient(api.Config{
		Address: address,
	})
	if err != nil {
		return nil, err
	}

	return &MetricCollector{
		name: name,
		api:  v1.NewAPI(client),
	}, nil
}

func (c *MetricCollector) Name() string {
	return c.name
}

func (c *MetricCollector) CollectMetric(ctx context.Context) (float64, error) {
	result, warnings, err := c.api.Query(context.Background(), c.query, time.Now())
	if err != nil {
		return 0, err
	} else if len(warnings) > 0 {
		slog.Warn("prometheus", slog.Any("warnings", warnings))
	}

	switch result := result.(type) {
	case model.Vector:
		if result.Len() < 1 {
			return 0, fmt.Errorf("empty prometheus result")
		}
		str := result[0].Value.String()

		v, err := strconv.ParseFloat(str, 64)
		if err != nil {
			return 0, fmt.Errorf("cannot parse prometheus result as float64: %q", str)
		}
		return v, nil

	default:
		return 0, fmt.Errorf("unexpected prometheus result type: %T", result)
	}
}
