package main

import (
	"context"
	"flag"
	"fmt"

	fas "github.com/superfly/fly-autoscaler"
	fasprom "github.com/superfly/fly-autoscaler/prometheus"
)

// EvalCommand represents a command to collect metrics and evaluate machine count.
// This is use as a test command when setting up or debugging the autoscaler.
type EvalCommand struct {
	// Target Fly.io organization & application name.
	OrgName string
	AppName string

	// Target machine count expression.
	Expr string

	// Prometheus settings.
	Prometheus struct {
		Address    string
		MetricName string
		Query      string
		Token      string
	}
}

func NewEvalCommand() *EvalCommand {
	return &EvalCommand{}
}

func (c *EvalCommand) Run(ctx context.Context, args []string) (err error) {
	if err := c.parseFlags(ctx, args); err != nil {
		return err
	}
	if c.OrgName == "" {
		return fmt.Errorf("org name required")
	} else if c.AppName == "" {
		return fmt.Errorf("app name required")
	}
	if c.Expr == "" {
		return fmt.Errorf("expression required")
	}

	if c.Prometheus.Address == "" {
		return fmt.Errorf("prometheus address required")
	} else if c.Prometheus.MetricName == "" {
		return fmt.Errorf("prometheus metric name required")
	} else if c.Prometheus.Query == "" {
		return fmt.Errorf("prometheus query required")
	}

	// Instantiate prometheus collector.
	collector, err := fasprom.NewMetricCollector(
		c.Prometheus.MetricName,
		c.Prometheus.Address,
		c.Prometheus.Query,
		c.Prometheus.Token,
	)
	if err != nil {
		return fmt.Errorf("cannot create prometheus client: %w", err)
	}

	// Instantiate reconciler and evaluate once.
	r := fas.NewReconciler(nil)
	r.Expr = c.Expr
	r.Collectors = []fas.MetricCollector{collector}

	if err := r.CollectMetrics(ctx); err != nil {
		return fmt.Errorf("metrics collection failed: %w", err)
	}

	targetN, err := r.MachineN()
	if err != nil {
		return fmt.Errorf("cannot calculate machine count: %w", err)
	}
	fmt.Println(targetN)

	return nil
}

func (c *EvalCommand) parseFlags(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("fly-autoscaler-serve", flag.ContinueOnError)
	registerOrgNameFlag(fs, &c.OrgName)
	registerAppNameFlag(fs, &c.AppName)
	registerPrometheusFlags(fs, &c.Prometheus.Address, &c.Prometheus.MetricName,
		&c.Prometheus.Query, &c.Prometheus.Token)
	registerExprFlag(fs, &c.Expr)
	fs.Usage = func() {
		fmt.Println(`
The eval command runs collects metrics once and evaluates the given expression.
No scaling is performed. This command should be used for testing & debugging.

Usage:

	fly-autoscaler eval [arguments]

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
