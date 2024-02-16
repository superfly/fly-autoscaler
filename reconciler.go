package fas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/expr-lang/expr"
	"github.com/superfly/fly-go"
)

const (
	DefaultReconcilerInterval = 15 * time.Second
)

type Reconciler struct {
	client  FlyClient
	metrics map[string]float64

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelCauseFunc

	// The frequency to run the reconciliation loop.
	Interval time.Duration

	// Expression used for calculating the number of machines to scale to.
	Expr string

	// List of collectors to fetch metric values from.
	Collectors []MetricCollector
}

func NewReconciler(client FlyClient) *Reconciler {
	r := &Reconciler{
		client:   client,
		metrics:  make(map[string]float64),
		Interval: DefaultReconcilerInterval,
	}
	r.ctx, r.cancel = context.WithCancelCause(context.Background())
	return r
}

// Start runs the reconciliation loop in a separate goroutine.
func (r *Reconciler) Start() {
	r.wg.Add(1)
	go func() { defer r.wg.Done(); r.loop() }()
}

// Stop cancels the internal context and waits for the reconcilation loop to stop.
func (r *Reconciler) Stop() {
	r.cancel(errors.New("reconciler closed"))
	r.wg.Wait()
}

func (r *Reconciler) loop() {
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

LOOP:
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			// Collect all the metrics first.
			for _, c := range r.Collectors {
				value, err := c.CollectMetric(r.ctx)
				if err != nil {
					slog.Error("cannot collect metric",
						slog.String("name", c.Name()),
						slog.Any("err", err))
					continue LOOP
				}
				r.SetValue(c.Name(), value)
			}

			// Compute the target machine count and scale up if needed.
			if err := r.Reconcile(r.ctx); err != nil {
				slog.Error("cannot reconcile", slog.Any("err", err))
				continue LOOP
			}
		}
	}
}

// Value returns the value of a named metric and whether the metric has been set.
func (r *Reconciler) Value(name string) (float64, bool) {
	v, ok := r.metrics[name]
	return v, ok
}

// SetValue sets the value of a named metric.
func (r *Reconciler) SetValue(name string, value float64) {
	r.metrics[name] = value
}

// Reconcile scales the number of machines up, if needed. Machines should shut
// themselves down to scale down. Returns the number of started machines, if any.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	// Compute number of machines based on expr & metrics
	targetN, err := r.MachineN()
	if err != nil {
		return fmt.Errorf("compute target machine count: %w", err)
	}

	// Fetch list of running machines.
	machines, err := r.client.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list machines: %w", err)
	}
	m := machinesByState(machines)

	// If the current machine count equals our target machine count then we have
	// nothing to do so we can exit.
	startedN := len(m[fly.MachineStateStarted])
	if startedN == targetN {
		return nil
	}

	// If we have more started machines than we need, the machines need to
	// shut themselves down if they have no activity. The scaler won't scale down.
	if startedN > targetN {
		slog.Debug("started machine count exceeds target, waiting for machines to shut down",
			slog.Int("started", startedN),
			slog.Int("target", targetN))
		return nil
	}

	slog.Info("begin scale up",
		slog.Int("started", startedN),
		slog.Int("target", targetN))

	// Let the user know if we don't have enough machines to reach the target count.
	diff := targetN - startedN
	stoppedN := len(m[fly.MachineStateStopped])
	if stoppedN < diff {
		slog.Warn("not enough stopped machines available to reach target, please create more machines",
			slog.Int("started", startedN),
			slog.Int("stopped", stoppedN),
			slog.Int("target", targetN))
	}

	// Sort stopped machines by an arbitrary value (ID) so results are deterministic.
	stopped := m[fly.MachineStateStopped]
	sort.Slice(stopped, func(i, j int) bool { return stopped[i].ID < stopped[j].ID })

	// Attempt to start as many machines as needed.
	var newlyStartedN int
	for _, machine := range stopped {
		_, err := r.client.Start(ctx, machine.ID, "")
		if err != nil {
			slog.Error("cannot start machine, skipping",
				slog.String("id", machine.ID),
				slog.Any("err", err))
			continue
		}

		slog.Info("machine started", slog.String("id", machine.ID))
		newlyStartedN++
	}

	slog.Info("scale up completed", slog.Int("n", newlyStartedN))

	return nil
}

// MachineN returns the number of machines to scale to based on Expr.
func (r *Reconciler) MachineN() (int, error) {
	env := map[string]any{
		"min": minFloat64,
		"max": maxFloat64,
	}
	for k, v := range r.metrics {
		env[k] = v
	}

	program, err := expr.Compile(r.Expr, expr.AsFloat64(), expr.Env(env))
	if err != nil {
		return 0, fmt.Errorf("compile expression: %w", err)
	}

	v, err := expr.Run(program, env)
	if err != nil {
		return 0, fmt.Errorf("execute expression: %w", err)
	}

	f := math.Round(v.(float64))
	if math.IsNaN(f) {
		return 0, ErrExprNaN
	} else if math.IsInf(f, 0) {
		return 0, ErrExprInf
	}
	return int(f), nil
}

func minFloat64(x, y float64) float64 {
	if x < y {
		return x
	}
	return y
}

func maxFloat64(x, y float64) float64 {
	if x > y {
		return x
	}
	return y
}

func machinesByState(a []*fly.Machine) map[string][]*fly.Machine {
	m := make(map[string][]*fly.Machine)
	for _, mach := range a {
		m[mach.State] = append(m[mach.State], mach)
	}
	return m
}
