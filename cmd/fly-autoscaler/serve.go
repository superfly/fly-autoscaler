package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	fas "github.com/superfly/fly-autoscaler"
)

// ServeCommand represents a command run the autoscaler server process.
type ServeCommand struct {
	reconciler *fas.Reconciler
	Config     *Config
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
	if err := c.Config.Validate(); err != nil {
		return err
	}

	// Instantiate client to scale up machines.
	client, err := c.Config.NewFlapsClient(ctx)
	if err != nil {
		return fmt.Errorf("cannot create flaps client: %w", err)
	}
	slog.Info("connected to flaps", slog.String("app_name", c.Config.AppName))

	// Instantiate prometheus collector.
	collectors, err := c.Config.NewMetricCollectors()
	if err != nil {
		return fmt.Errorf("cannot create metrics collectors: %w", err)
	}
	slog.Info("metrics collectors initialized", slog.Int("n", len(collectors)))

	// Instantiate and start reconcilation.
	r := fas.NewReconciler(client)
	r.MinCreatedMachineN = c.Config.GetMinCreatedMachineN()
	r.MaxCreatedMachineN = c.Config.GetMaxCreatedMachineN()
	r.MinStartedMachineN = c.Config.GetMinStartedMachineN()
	r.MaxStartedMachineN = c.Config.GetMaxStartedMachineN()
	r.Interval = c.Config.Interval
	r.Collectors = collectors
	r.RegisterPromMetrics(prometheus.DefaultRegisterer)
	c.reconciler = r

	slog.Info("beginning reconciliation loop")
	r.Start()

	go c.serveMetricsServer(ctx)

	return nil
}

func (c *ServeCommand) serveMetricsServer(ctx context.Context) {
	addr := ":9090"
	slog.Info("serving metrics", slog.String("addr", addr))
	http.Handle("/metrics", promhttp.Handler())
	if err := http.ListenAndServe(addr, nil); err != nil && ctx.Err() == nil {
		slog.Error("cannot serve metrics", slog.Any("err", err))
	}
}

func (c *ServeCommand) parseFlags(ctx context.Context, args []string) (err error) {
	fs := flag.NewFlagSet("fly-autoscaler-serve", flag.ContinueOnError)
	configPath := registerConfigPathFlag(fs)
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

	if c.Config, err = NewConfigFromEnv(); err != nil {
		return err
	}
	if *configPath != "" {
		if err := ParseConfigFromFile(*configPath, c.Config); err != nil {
			return err
		}
	}

	// Initialize logging.
	hopt := &slog.HandlerOptions{Level: slog.LevelInfo, ReplaceAttr: removeSlogTime}
	if c.Config.Verbose {
		hopt.Level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, hopt)))

	return nil
}

func removeSlogTime(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && len(groups) == 0 {
		return slog.Attr{}
	}
	return a
}
