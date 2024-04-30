package prometheus

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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

func NewMetricCollector(name, address, query, token string) (*MetricCollector, error) {
	client, err := newHTTPClient(api.Config{
		Address: address,
	}, token)
	if err != nil {
		return nil, err
	}

	return &MetricCollector{
		name:  name,
		query: query,
		api:   v1.NewAPI(client),
	}, nil
}

func (c *MetricCollector) Name() string {
	return c.name
}

func (c *MetricCollector) CollectMetric(ctx context.Context, app string) (float64, error) {
	query := fas.ExpandMetricQuery(ctx, c.query, app)

	result, warnings, err := c.api.Query(context.Background(), query, time.Now())
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

type httpClient struct {
	api.Client
	token string
}

func newHTTPClient(cfg api.Config, token string) (api.Client, error) {
	c, err := api.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	return &httpClient{Client: c, token: token}, nil
}

func (c *httpClient) Do(ctx context.Context, req *http.Request) (*http.Response, []byte, error) {
	if strings.HasPrefix(c.token, "Fly") { // macaroons
		req.Header.Set("Authorization", c.token)
	} else if c.token != "" { // auth token
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	return c.Client.Do(ctx, req)
}
