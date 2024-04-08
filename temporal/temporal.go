package temporal

import (
	"context"
	"crypto/tls"

	"github.com/superfly/fly-autoscaler"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

var _ fas.MetricCollector = (*MetricCollector)(nil)

type MetricCollector struct {
	name   string
	client client.Client

	// Host & port of the Temporal server. Defaults to localhost:7233.
	// Must be set before calling Open().
	Hostport string

	// Namespace to connect to. Defaults to "default".
	// Must be set before calling Open().
	Namespace string

	// Certificate & key data. Optional.
	// Must be set before calling Open().
	Cert []byte
	Key  []byte

	// Query string used to filter running workflows.
	Query string
}

func NewMetricCollector(name string) *MetricCollector {
	return &MetricCollector{name: name}
}

func (c *MetricCollector) Open() (err error) {
	opt := client.Options{
		HostPort:  c.Hostport,
		Namespace: c.Namespace,
	}

	if len(c.Cert) != 0 || len(c.Key) != 0 {
		cert, err := tls.X509KeyPair(c.Cert, c.Key)
		if err != nil {
			return err
		}
		opt.Credentials = client.NewMTLSCredentials(cert)
	}

	c.client, err = client.Dial(opt)

	return err
}

func (c *MetricCollector) Close() error {
	if c.client != nil {
		c.client.Close()
	}
	return nil
}

func (c *MetricCollector) Name() string {
	return c.name
}

func (c *MetricCollector) CollectMetric(ctx context.Context) (float64, error) {
	// Append additional query filter, if specified.
	query := `ExecutionStatus="Running"`
	if c.Query != "" {
		query += " AND (" + c.Query + ")"
	}

	resp, err := c.client.CountWorkflow(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Query: query,
	})
	if err != nil {
		return 0, err
	}
	return float64(resp.Count), nil
}
