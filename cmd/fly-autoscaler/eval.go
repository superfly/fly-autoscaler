package main

import (
	"context"
	"flag"
	"fmt"

	fas "github.com/superfly/fly-autoscaler"
)

// EvalCommand represents a command to collect metrics and evaluate machine count.
// This is use as a test command when setting up or debugging the autoscaler.
type EvalCommand struct {
	Config *Config
}

func NewEvalCommand() *EvalCommand {
	return &EvalCommand{}
}

func (c *EvalCommand) Run(ctx context.Context, args []string) (err error) {
	if err := c.parseFlags(ctx, args); err != nil {
		return err
	}
	if err := c.Config.Validate(); err != nil {
		return err
	}

	collectors, err := c.Config.NewMetricCollectors()
	if err != nil {
		return fmt.Errorf("cannot create metrics collectors: %w", err)
	}

	// Instantiate reconciler and evaluate once.
	r := fas.NewReconciler(nil)
	r.Expr = c.Config.Expr
	r.Collectors = collectors

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
	configPath := registerConfigPathFlag(fs)
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

	if c.Config, err = NewConfigFromEnv(); err != nil {
		return err
	}
	if *configPath != "" {
		if err := ParseConfigFromFile(*configPath, c.Config); err != nil {
			return err
		}
	}

	return nil
}
