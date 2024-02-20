package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"
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

	// Instantiate prometheus collector.
	collectors, err := c.Config.NewMetricCollectors()
	if err != nil {
		return fmt.Errorf("cannot create metrics collectors: %w", err)
	}

	// Instantiate and start reconcilation.
	r := fas.NewReconciler(client)
	r.Expr = c.Config.Expr
	r.Interval = c.Config.Interval
	r.Collectors = collectors
	r.RegisterPromMetrics(prometheus.DefaultRegisterer)
	c.reconciler = r

	r.Start()

	return nil
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
	hopt := &slog.HandlerOptions{Level: slog.LevelInfo}
	if c.Config.Verbose {
		hopt.Level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, hopt)))

	return nil
}
