package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-autoscaler/prometheus"
	"github.com/superfly/fly-go/flaps"
)

// ServeCommand represents a command run the autoscaler server process.
type ServeCommand struct {
	reconciler *fas.Reconciler

	// Target Fly.io application name.
	AppName string

	// Target machine count expression.
	Expr string

	// Reconciliation interval.
	Interval time.Duration

	// Prometheus settings.
	Prometheus struct {
		Address string
		Name    string
		Query   string
	}
}

func NewServeCommand() *ServeCommand {
	return &ServeCommand{}
}

func (c *ServeCommand) Close() (err error) {
	if c.reconciler != nil {
		c.reconciler.Stop()
	}
	return nil
}

func (c *ServeCommand) Run(ctx context.Context, args []string) (err error) {
	if err := c.parseFlags(ctx, args); err != nil {
		return err
	}
	if c.AppName == "" {
		return fmt.Errorf("app name required")
	}
	if c.Expr == "" {
		return fmt.Errorf("expression required")
	}

	if c.Prometheus.Address == "" {
		return fmt.Errorf("prometheus address required")
	} else if c.Prometheus.Name == "" {
		return fmt.Errorf("prometheus metric name required")
	} else if c.Prometheus.Query == "" {
		return fmt.Errorf("prometheus query required")
	}

	// Instantiate client to scale up machines.
	client, err := flaps.NewWithOptions(ctx, flaps.NewClientOpts{AppName: c.AppName})
	if err != nil {
		return fmt.Errorf("cannot create flaps client: %w", err)
	}

	// Instantiate prometheus collector.
	collector, err := prometheus.NewMetricCollector(
		c.Prometheus.Name,
		c.Prometheus.Address,
		c.Prometheus.Query,
	)
	if err != nil {
		return fmt.Errorf("cannot create prometheus client: %w", err)
	}

	// Instantiate and start reconcilation.
	r := fas.NewReconciler(client)
	r.Expr = c.Expr
	r.Interval = c.Interval
	r.Collectors = []fas.MetricCollector{collector}

	return nil
}

func (c *ServeCommand) parseFlags(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("fly-autoscaler-serve", flag.ContinueOnError)
	fs.StringVar(&c.AppName, "app-name", os.Getenv("FAS_APP_NAME"), "fly app name")
	fs.DurationVar(&c.Interval, "interval", fas.DefaultReconcilerInterval, "reconciliation interval")
	fs.StringVar(&c.Prometheus.Address, "prometheus-address", os.Getenv("FAS_PROMETHEUS_ADDRESS"), "prometheus server address")
	fs.StringVar(&c.Prometheus.Name, "prometheus-name", os.Getenv("FAS_PROMETHEUS_NAME"), "prometheus metric name")
	fs.StringVar(&c.Prometheus.Query, "prometheus-query", os.Getenv("FAS_PROMETHEUS_QUERY"), "prometheus scalar query")
	fs.Usage = func() {
		fmt.Println(`
The serve command runs the autoscaler server process and begins managing a fleet
of Fly machines based on metrics.

Usage:

	fly-autoscaler serve [arguments]

Arguments:
`[1:])
		fs.PrintDefaults()
		fmt.Println("")
	}
	if err := fs.Parse(args); err != nil {
		return err
	} else if fs.NArg() > 0 {
		return fmt.Errorf("too many arguments")
	}

	return nil
}
