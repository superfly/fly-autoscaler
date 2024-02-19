package fas

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/expr-lang/expr"
	"github.com/prometheus/client_golang/prometheus"
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

	// Must also be registered in RegisterPromMetrics() for visibility.
	Stats struct {
		MachineStarted     atomic.Int64
		MachineStartFailed atomic.Int64
		ScaleUp            atomic.Int64
		ScaleDown          atomic.Int64
		NoScale            atomic.Int64
	}
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
			if err := r.CollectMetrics(r.ctx); err != nil {
				slog.Error("metrics collection failed", slog.Any("err", err))
				continue LOOP
			}

			if err := r.Reconcile(r.ctx); err != nil {
				slog.Error("reconciliation failed", slog.Any("err", err))
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

// CollectMetrics fetches metrics from all collectors.
func (r *Reconciler) CollectMetrics(ctx context.Context) error {
	for _, c := range r.Collectors {
		value, err := c.CollectMetric(r.ctx)
		if err != nil {
			return fmt.Errorf("collect metric (%q): %w", c.Name(), err)
		}
		r.SetValue(c.Name(), value)
	}
	return nil
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
		r.Stats.NoScale.Add(1)
		return nil
	}

	// If we have more started machines than we need, the machines need to
	// shut themselves down if they have no activity. The scaler won't scale down.
	if startedN > targetN {
		r.Stats.ScaleDown.Add(1)

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
	remaining := diff
	for _, machine := range stopped {
		if remaining <= 0 {
			break
		}

		if err := r.startMachine(ctx, machine.ID); err != nil {
			slog.Error("cannot start machine, skipping",
				slog.String("id", machine.ID),
				slog.Any("err", err))
			continue
		}

		slog.Info("machine started", slog.String("id", machine.ID))
		remaining--
	}

	newlyStartedN := diff - remaining
	slog.Info("scale up completed", slog.Int("n", newlyStartedN))

	r.Stats.ScaleUp.Add(1)

	return nil
}

func (r *Reconciler) startMachine(ctx context.Context, id string) error {
	if _, err := r.client.Start(ctx, id, ""); err != nil {
		r.Stats.MachineStartFailed.Add(1)
		return err
	}
	r.Stats.MachineStarted.Add(1)
	return nil
}

// MachineN returns the number of machines to scale to based on Expr.
func (r *Reconciler) MachineN() (int, error) {
	env := map[string]any{}
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

	if f < 0 {
		return 0, nil
	}
	return int(f), nil
}

func (r *Reconciler) RegisterPromMetrics(reg prometheus.Registerer) {
	r.registerMachineStartCount(reg)
	r.registerReconcileCount(reg)
}

func (r *Reconciler) registerMachineStartCount(reg prometheus.Registerer) {
	const name = "fas_machine_start_count"

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "started"},
		},
		func() float64 { return float64(r.Stats.MachineStarted.Load()) },
	))
	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        "fas_machine_start_count",
			ConstLabels: prometheus.Labels{"status": "failed"},
		},
		func() float64 { return float64(r.Stats.MachineStartFailed.Load()) },
	))
}

func (r *Reconciler) registerReconcileCount(reg prometheus.Registerer) {
	const name = "fas_reconcile_count"

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "scale_up"},
		},
		func() float64 { return float64(r.Stats.ScaleUp.Load()) },
	))

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "scale_down"},
		},
		func() float64 { return float64(r.Stats.ScaleDown.Load()) },
	))

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "no_scale"},
		},
		func() float64 { return float64(r.Stats.NoScale.Load()) },
	))
}

func machinesByState(a []*fly.Machine) map[string][]*fly.Machine {
	m := make(map[string][]*fly.Machine)
	for _, mach := range a {
		m[mach.State] = append(m[mach.State], mach)
	}
	return m
}
