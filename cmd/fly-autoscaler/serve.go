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
	pool   *fas.ReconcilerPool
	Config *Config
}

func NewServeCommand() *ServeCommand {
	return &ServeCommand{}
}

func (c *ServeCommand) Close() (err error) {
	if c.pool != nil {
		if err := c.pool.Close(); err != nil {
			slog.Warn("failed to close reconciler pool", slog.Any("err", err))
		}
	}
	return nil
}

func (c *ServeCommand) Run(ctx context.Context, args []string) (err error) {
	slog.Info("fly-autoscaler",
		slog.String("version", Version),
		slog.String("commit", Commit))

	if err := c.parseFlags(ctx, args); err != nil {
		return err
	}
	if err := c.Config.Validate(); err != nil {
		return err
	}

	// Instantiate clients for access org/apps & for scaling machines.
	flyClient, err := c.Config.NewFlyClient(ctx)
	if err != nil {
		return fmt.Errorf("cannot create fly client: %w", err)
	}
	slog.Info("connected to fly")

	// Instantiate prometheus collector.
	collectors, err := c.Config.NewMetricCollectors()
	if err != nil {
		return fmt.Errorf("cannot create metrics collectors: %w", err)
	}
	slog.Info("metrics collectors initialized", slog.Int("n", len(collectors)))

	minCreatedMachineN := c.Config.GetMinCreatedMachineN()
	maxCreatedMachineN := c.Config.GetMaxCreatedMachineN()
	minStartedMachineN := c.Config.GetMinStartedMachineN()
	maxStartedMachineN := c.Config.GetMaxStartedMachineN()

	// Instantiate pool.
	p := fas.NewReconcilerPool(flyClient, c.Config.Concurrency)
	if p.NewFlapsClient, err = c.Config.NewFlapsClient(); err != nil {
		return fmt.Errorf("cannot initialize flaps client constructor: %w", err)
	}
	p.NewReconciler = func() *fas.Reconciler {
		r := fas.NewReconciler()
		r.MinCreatedMachineN = minCreatedMachineN
		r.MaxCreatedMachineN = maxCreatedMachineN
		r.MinStartedMachineN = minStartedMachineN
		r.MaxStartedMachineN = maxStartedMachineN
		r.InitialMachineState = c.Config.InitialMachineState
		r.Regions = c.Config.Regions
		r.ProcessGroup = c.Config.ProcessGroup
		r.Collectors = collectors
		return r
	}
	p.AppName = c.Config.AppName
	p.OrganizationSlug = c.Config.Org
	p.ReconcileInterval = c.Config.Interval
	p.ReconcileTimeout = c.Config.Timeout
	p.AppListRefreshInterval = c.Config.AppListRefreshInterval
	p.RegisterPromMetrics(prometheus.DefaultRegisterer)
	c.pool = p

	attrs := []any{
		slog.String("interval", p.ReconcileInterval.String()),
		slog.String("timeout", p.ReconcileTimeout.String()),
		slog.String("appListRefreshInterval", p.AppListRefreshInterval.String()),
		slog.Int("collectors", len(collectors)),
	}

	if regions := c.Config.Regions; len(regions) > 0 {
		attrs = append(attrs, slog.Any("regions", regions))
	}

	if minCreatedMachineN == maxCreatedMachineN {
		attrs = append(attrs, slog.String("created", minCreatedMachineN))
	} else if minCreatedMachineN != "" || maxCreatedMachineN != "" {
		attrs = append(attrs, slog.Group("created",
			slog.String("min", minCreatedMachineN),
			slog.String("max", maxCreatedMachineN),
		))
	}

	if minStartedMachineN == maxStartedMachineN {
		attrs = append(attrs, slog.String("started", minStartedMachineN))
	} else if minStartedMachineN != "" || maxStartedMachineN != "" {
		attrs = append(attrs, slog.Group("started",
			slog.String("min", minStartedMachineN),
			slog.String("max", maxStartedMachineN),
		))
	}

	slog.Info("reconciler pool initialized, beginning loop", attrs...)
	if err := p.Open(); err != nil {
		return fmt.Errorf("cannot initialize reconciler pool: %w", err)
	}

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
