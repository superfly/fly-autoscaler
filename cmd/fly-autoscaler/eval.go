package main

import (
	"context"
	"encoding/json"
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
	r := fas.NewReconciler()
	r.MinCreatedMachineN = c.Config.GetMinCreatedMachineN()
	r.MaxCreatedMachineN = c.Config.GetMaxCreatedMachineN()
	r.MinStartedMachineN = c.Config.GetMinStartedMachineN()
	r.MaxStartedMachineN = c.Config.GetMaxStartedMachineN()
	r.Collectors = collectors

	if err := r.CollectMetrics(ctx); err != nil {
		return fmt.Errorf("metrics collection failed: %w", err)
	}

	var out evalOutput
	if v, ok, err := r.CalcMinCreatedMachineN(); err != nil {
		return fmt.Errorf("cannot calculate min created machine count: %w", err)
	} else if ok {
		out.Created.Min = &v
	}

	if v, ok, err := r.CalcMaxCreatedMachineN(); err != nil {
		return fmt.Errorf("cannot calculate max created machine count: %w", err)
	} else if ok {
		out.Created.Max = &v
	}

	if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
		return fmt.Errorf("cannot calculate min started machine count: %w", err)
	} else if ok {
		out.Started.Min = &v
	}

	if v, ok, err := r.CalcMaxStartedMachineN(); err != nil {
		return fmt.Errorf("cannot calculate max started machine count: %w", err)
	} else if ok {
		out.Started.Max = &v
	}

	buf, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(buf))

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

type evalOutput struct {
	Created struct {
		Min *int `json:"min"`
		Max *int `json:"max"`
	} `json:"created"`

	Started struct {
		Min *int `json:"min"`
		Max *int `json:"max"`
	} `json:"started"`
}
