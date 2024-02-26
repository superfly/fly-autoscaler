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

// Reconciler defaults.
const (
	DefaultReconcilerInterval = 15 * time.Second
)

// Reconciler represents the central part of the autoscaler that stores metrics,
// computes the number of necessary machines, and performs scaling.
type Reconciler struct {
	client  FlyClient
	metrics map[string]float64

	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelCauseFunc

	// The frequency to run the reconciliation loop.
	Interval time.Duration

	// Expression used for calculating the
	MinStartedMachineN string
	MaxStartedMachineN string

	// List of collectors to fetch metric values from.
	Collectors []MetricCollector

	// Must also be registered in RegisterPromMetrics() for visibility.
	Stats struct {
		// Outcomes, incremented for each reconciliation.
		ScaleUp   atomic.Int64
		ScaleDown atomic.Int64
		NoScale   atomic.Int64

		// Individual machine stats.
		MachineStarted     atomic.Int64
		MachineStartFailed atomic.Int64
		MachineStopped     atomic.Int64
		MachineStopFailed  atomic.Int64
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
	errReconciliationTimeout := fmt.Errorf("reconciliation timeout")

	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			func() {
				ctx, cancel := context.WithTimeoutCause(r.ctx, r.Interval, errReconciliationTimeout)
				defer cancel()

				if err := r.CollectMetrics(ctx); err != nil {
					slog.Error("metrics collection failed", slog.Any("err", err))
					return
				}

				if err := r.Reconcile(ctx); err != nil {
					slog.Error("reconciliation failed", slog.Any("err", err))
					return
				}
			}()
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
	minStartedN, err := r.CalcMinStartedMachineN()
	if err != nil {
		return fmt.Errorf("compute minimum started machine count: %w", err)
	}
	maxStartedN, err := r.CalcMaxStartedMachineN()
	if err != nil {
		return fmt.Errorf("compute minimum started machine count: %w", err)
	}

	// Fetch list of running machines.
	machines, err := r.client.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list machines: %w", err)
	}
	m := machinesByState(machines)

	// Log out stats so we know exactly what the state of the world is.
	slog.Info("reconciling",
		slog.Group("current",
			slog.Int("started", len(m[fly.MachineStateStarted])),
			slog.Int("stopped", len(m[fly.MachineStateStopped])),
		),
		slog.Group("target",
			slog.Group("started",
				slog.Int("min", minStartedN),
				slog.Int("max", maxStartedN),
			),
		),
	)

	// Determine if we need to scale up or down.
	startedN := len(m[fly.MachineStateStarted])
	if startedN > maxStartedN {
		return r.scaleDown(ctx, m[fly.MachineStateStarted], startedN-maxStartedN)
	} else if startedN < minStartedN {
		return r.scaleUp(ctx, m[fly.MachineStateStopped], minStartedN-startedN)
	}

	r.Stats.NoScale.Add(1)
	return nil
}

func (r *Reconciler) scaleDown(ctx context.Context, startedMachines []*fly.Machine, n int) error {
	r.Stats.ScaleDown.Add(1)

	slog.Info("begin scale down")

	// Sort stopped machines by an arbitrary value (ID) so results are deterministic.
	sort.Slice(startedMachines, func(i, j int) bool { return startedMachines[i].ID < startedMachines[j].ID })

	// Attempt to stop as many machines as needed.
	remaining := n
	for _, machine := range startedMachines {
		if remaining <= 0 {
			break
		}

		if err := r.stopMachine(ctx, machine.ID); err != nil {
			slog.Error("cannot stop machine, skipping",
				slog.String("id", machine.ID),
				slog.Any("err", err))
			continue
		}

		slog.Info("machine stopped", slog.String("id", machine.ID))
		remaining--
	}

	newlyStoppedN := n - remaining
	slog.Info("scale down completed", slog.Int("n", newlyStoppedN))

	return nil
}

func (r *Reconciler) scaleUp(ctx context.Context, stoppedMachines []*fly.Machine, n int) error {
	r.Stats.ScaleUp.Add(1)

	slog.Info("begin scale up")

	// Let the user know if we don't have enough machines to reach the target count.
	if len(stoppedMachines) < n {
		slog.Warn("not enough stopped machines available to reach target, please create more machines")
	}

	// Sort stopped machines by an arbitrary value (ID) so results are deterministic.
	sort.Slice(stoppedMachines, func(i, j int) bool { return stoppedMachines[i].ID < stoppedMachines[j].ID })

	// Attempt to start as many machines as needed.
	remaining := n
	for _, machine := range stoppedMachines {
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

	newlyStartedN := n - remaining
	slog.Info("scale up completed", slog.Int("n", newlyStartedN))

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

func (r *Reconciler) stopMachine(ctx context.Context, id string) error {
	if err := r.client.Stop(ctx, fly.StopMachineInput{ID: id}, ""); err != nil {
		r.Stats.MachineStopFailed.Add(1)
		return err
	}
	r.Stats.MachineStopped.Add(1)
	return nil
}

// CalcMinStartedMachineN returns the number of minimum number of started machines.
func (r *Reconciler) CalcMinStartedMachineN() (int, error) {
	return r.evalInt(r.MinStartedMachineN)
}

// CalcMaxStartedMachineN returns the number of maximum number of started machines.
func (r *Reconciler) CalcMaxStartedMachineN() (int, error) {
	return r.evalInt(r.MaxStartedMachineN)
}

// evalInt compiles & runs an expression. Returns a rounded integer.
func (r *Reconciler) evalInt(s string) (int, error) {
	if s == "" {
		return 0, ErrExprRequired
	}

	env := map[string]any{}
	for k, v := range r.metrics {
		env[k] = v
	}

	program, err := expr.Compile(s, expr.AsFloat64(), expr.Env(env))
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
	r.registerMachineStoppedCount(reg)
	r.registerReconcileCount(reg)
}

func (r *Reconciler) registerMachineStartCount(reg prometheus.Registerer) {
	const name = "fas_machine_start_count"

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "ok"},
		},
		func() float64 { return float64(r.Stats.MachineStarted.Load()) },
	))
	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "failed"},
		},
		func() float64 { return float64(r.Stats.MachineStartFailed.Load()) },
	))
}

func (r *Reconciler) registerMachineStoppedCount(reg prometheus.Registerer) {
	const name = "fas_machine_stop_count"

	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "ok"},
		},
		func() float64 { return float64(r.Stats.MachineStopped.Load()) },
	))
	reg.MustRegister(prometheus.NewCounterFunc(
		prometheus.CounterOpts{
			Name:        name,
			ConstLabels: prometheus.Labels{"status": "failed"},
		},
		func() float64 { return float64(r.Stats.MachineStopFailed.Load()) },
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
