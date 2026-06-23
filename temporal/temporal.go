package temporal

import (
	"context"
	"crypto/tls"
	"fmt"

	fas "github.com/superfly/fly-autoscaler"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

var _ fas.MetricCollector = (*MetricCollector)(nil)

type MetricCollector struct {
	name   string
	client client.Client

	// Host & port of the Temporal server. Must be set before calling Open().
	Address string

	// Namespace to connect to. Must be set before calling Open().
	Namespace string

	// Certificate & key data. Optional. Must be set before calling Open().
	Cert []byte
	Key  []byte

	// APIKey is the API key to use for the Temporal server. Optional.
	APIKey string

	// Query string used to filter running workflows.
	Query string
}

func NewMetricCollector(name string) *MetricCollector {
	return &MetricCollector{name: name}
}

func (c *MetricCollector) Open() (err error) {
	if c.Address == "" {
		return fmt.Errorf("temporal address required")
	}
	if c.Namespace == "" {
		return fmt.Errorf("temporal namespace required")
	}

	opt := client.Options{
		HostPort:  c.Address,
		Namespace: c.Namespace,
	}

	if len(c.Cert) != 0 || len(c.Key) != 0 {
		cert, err := tls.X509KeyPair(c.Cert, c.Key)
		if err != nil {
			return err
		}
		opt.ConnectionOptions.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	}

	if c.APIKey != "" {
		opt.ConnectionOptions = client.ConnectionOptions{
			TLS: &tls.Config{},
			DialOptions: []grpc.DialOption{
				grpc.WithUnaryInterceptor(
					func(ctx context.Context, method string, req any, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
						return invoker(
							metadata.AppendToOutgoingContext(ctx, "temporal-namespace", c.Namespace),
							method,
							req,
							reply,
							cc,
							opts...,
						)
					},
				),
			},
		}

		opt.Credentials = client.NewAPIKeyStaticCredentials(c.APIKey)
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

func (c *MetricCollector) CollectMetric(ctx context.Context, app string) (float64, error) {
	// Append additional query filter, if specified.
	query := `ExecutionStatus="Running"`
	if c.Query != "" {
		query += " AND (" + c.Query + ")"
	}

	query = fas.ExpandMetricQuery(ctx, query, app)

	resp, err := c.client.CountWorkflow(ctx, &workflowservice.CountWorkflowExecutionsRequest{
		Query: query,
	})
	if err != nil {
		return 0, err
	}
	return float64(resp.Count), nil
}
