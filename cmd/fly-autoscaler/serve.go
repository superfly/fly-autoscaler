package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	fas "github.com/superfly/fly-autoscaler"
	fasprom "github.com/superfly/fly-autoscaler/prometheus"
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
		Address    string
		MetricName string
		Query      string
		Token      string
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
	} else if c.Prometheus.MetricName == "" {
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
	collector, err := fasprom.NewMetricCollector(
		c.Prometheus.MetricName,
		c.Prometheus.Address,
		c.Prometheus.Query,
		c.Prometheus.Token,
	)
	if err != nil {
		return fmt.Errorf("cannot create prometheus client: %w", err)
	}

	// Instantiate and start reconcilation.
	r := fas.NewReconciler(client)
	r.Expr = c.Expr
	r.Interval = c.Interval
	r.Collectors = []fas.MetricCollector{collector}
	r.RegisterPromMetrics(prometheus.DefaultRegisterer)
	c.reconciler = r

	r.Start()

	return nil
}

func (c *ServeCommand) parseFlags(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("fly-autoscaler-serve", flag.ContinueOnError)
	registerAppNameFlag(fs, &c.AppName)
	registerPrometheusFlags(fs, &c.Prometheus.Address, &c.Prometheus.MetricName,
		&c.Prometheus.Query, &c.Prometheus.Token)
	registerExprFlag(fs, &c.Expr)
	fs.DurationVar(&c.Interval, "interval", fas.DefaultReconcilerInterval, "Reconciliation interval")
	verbose := fs.Bool("verbose", false, "Enable verbose logging")
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

	// Initialize logging.
	hopt := &slog.HandlerOptions{Level: slog.LevelInfo}
	if *verbose {
		hopt.Level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, hopt)))

	return nil
}
