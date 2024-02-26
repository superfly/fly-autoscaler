package fas_test

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-autoscaler/mock"
	"github.com/superfly/fly-go"
)

func init() {
	slog.SetDefault(slog.New(
		slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}),
	))
}

func TestReconciler_Value(t *testing.T) {
	t.Run("SetValue", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.SetValue("foo", 100)
		if v, ok := r.Value("foo"); !ok {
			t.Fatal("expected value")
		} else if got, want := v, 100.0; got != want {
			t.Fatalf("foo=%v, want %v", got, want)
		}
	})

	t.Run("NoValue", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		if _, ok := r.Value("foo"); ok {
			t.Fatal("expected no value")
		}
	})
}

func TestReconciler_MinStartedMachineN(t *testing.T) {
	t.Run("Constant", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "1"
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 1; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Round", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "2.6"
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 3; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Var", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "x + y + 2"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 13; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Min", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "min(x, y)"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 4; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Max", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "max(x, y)"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 7; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Neg", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "-2"
		if v, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if got, want := v, 0; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("NaN", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "x + 1"
		r.SetValue("x", math.NaN())
		if _, err := r.CalcMinStartedMachineN(); err == nil || err != fas.ErrExprNaN {
			t.Fatal(err)
		}
	})

	t.Run("Inf", func(t *testing.T) {
		r := fas.NewReconciler(nil)
		r.MinStartedMachineN = "1 / 0"
		if _, err := r.CalcMinStartedMachineN(); err == nil || err != fas.ErrExprInf {
			t.Fatal(err)
		}
	})
}

func TestReconciler_Scale(t *testing.T) {
	// Ensure that if the target count and started count are the same, there
	// will not be any new machines started.
	t.Run("NoScale", func(t *testing.T) {
		var client mock.FlyClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStarted},
				{ID: "2", State: fly.MachineStateStopped},
			}, nil
		}
		client.StartFunc = func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
			t.Fatal("expected no start")
			return &fly.MachineStartResponse{}, nil
		}

		r := fas.NewReconciler(&client)
		r.MinStartedMachineN = "1"
		r.MaxStartedMachineN = "2"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := r.Stats.NoScale.Load(), int64(1); got != want {
			t.Fatalf("NoScale=%v, want %v", got, want)
		}
	})

	// Ensure that number of machines will be scaled up to match target number.
	t.Run("ScaleUp", func(t *testing.T) {
		var invokeStartN int
		var client mock.FlyClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStarted},
				{ID: "2", State: fly.MachineStateStopped},
				{ID: "3", State: fly.MachineStateStopped},
				{ID: "4", State: fly.MachineStateStopped},
			}, nil
		}
		client.StartFunc = func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
			switch id {
			case "2", "3":
				invokeStartN++
			default:
				t.Fatalf("unexpected start id: %v", id)
			}
			return &fly.MachineStartResponse{}, nil
		}

		r := fas.NewReconciler(&client)
		r.MinStartedMachineN = "foo + 2"
		r.MaxStartedMachineN = r.MinStartedMachineN
		r.SetValue("foo", 1.0)
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeStartN, 2; got != want {
			t.Fatalf("startN=%v, want %v", got, want)
		} else if got, want := r.Stats.ScaleUp.Load(), int64(1); got != want {
			t.Fatalf("ScaleUp=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStarted.Load(), int64(2); got != want {
			t.Fatalf("MachineStarted=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStartFailed.Load(), int64(0); got != want {
			t.Fatalf("MachineStartFailed=%v, want %v", got, want)
		}
	})

	// Ensure that the reconciler will keep trying to start machines if one fails.
	t.Run("StartFailed", func(t *testing.T) {
		var invokeStartN int
		var client mock.FlyClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStopped},
				{ID: "2", State: fly.MachineStateStopped},
				{ID: "3", State: fly.MachineStateStopped},
				{ID: "4", State: fly.MachineStateStopped},
			}, nil
		}
		client.StartFunc = func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
			invokeStartN++
			switch id {
			case "1", "3":
				// ok
			case "2":
				return nil, fmt.Errorf("marker")
			default:
				t.Fatalf("unexpected start id: %v", id)
			}
			return &fly.MachineStartResponse{}, nil
		}

		r := fas.NewReconciler(&client)
		r.MinStartedMachineN = "2"
		r.MaxStartedMachineN = r.MinStartedMachineN
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeStartN, 3; got != want {
			t.Fatalf("startN=%v, want %v", got, want)
		} else if got, want := r.Stats.ScaleUp.Load(), int64(1); got != want {
			t.Fatalf("ScaleUp=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStarted.Load(), int64(2); got != want {
			t.Fatalf("MachineStarted=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStartFailed.Load(), int64(1); got != want {
			t.Fatalf("MachineStartFailed=%v, want %v", got, want)
		}
	})

	// The reconciler should stop machines when they are above the max count.
	t.Run("ScaleDown", func(t *testing.T) {
		var client mock.FlyClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStarted},
				{ID: "2", State: fly.MachineStateStarted},
				{ID: "3", State: fly.MachineStateStarted},
				{ID: "4", State: fly.MachineStateStopped},
			}, nil
		}
		client.StopFunc = func(ctx context.Context, in fly.StopMachineInput, nonce string) error {
			switch in.ID {
			case "1", "2":
				// ok
			default:
				t.Fatalf("unexpected start id: %v", in.ID)
			}
			return nil
		}

		r := fas.NewReconciler(&client)
		r.MinStartedMachineN = "1"
		r.MaxStartedMachineN = "1"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := r.Stats.ScaleDown.Load(), int64(1); got != want {
			t.Fatalf("ScaleDown=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStopped.Load(), int64(2); got != want {
			t.Fatalf("MachineStopped=%v, want %v", got, want)
		}
	})

	t.Run("StopFailed", func(t *testing.T) {
		var client mock.FlyClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStarted},
				{ID: "2", State: fly.MachineStateStarted},
				{ID: "3", State: fly.MachineStateStarted},
				{ID: "4", State: fly.MachineStateStopped},
			}, nil
		}
		client.StopFunc = func(ctx context.Context, in fly.StopMachineInput, nonce string) error {
			switch in.ID {
			case "1", "3":
				// ok
			case "2":
				return fmt.Errorf("marker")
			default:
				t.Fatalf("unexpected start id: %v", in.ID)
			}
			return nil
		}

		r := fas.NewReconciler(&client)
		r.MinStartedMachineN = "1"
		r.MaxStartedMachineN = "1"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := r.Stats.MachineStopped.Load(), int64(2); got != want {
			t.Fatalf("MachineStopped=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStopFailed.Load(), int64(1); got != want {
			t.Fatalf("MachineStopFailed=%v, want %v", got, want)
		}
	})
}

// Ensure prometheus registration does not blow up.
func TestReconciler_RegisterPromMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fas.NewReconciler(nil).RegisterPromMetrics(reg)
	if _, err := reg.Gather(); err != nil {
		t.Fatal(err)
	}
}

// Ensure that server can collect metrics and run reconcilation on a loop.
func TestReconciler_StartStop(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode enabled, skipping")
	}

	var mu sync.Mutex
	machines := []*fly.Machine{
		{ID: "1", State: fly.MachineStateStopped},
		{ID: "2", State: fly.MachineStateStopped},
		{ID: "3", State: fly.MachineStateStopped},
		{ID: "4", State: fly.MachineStateStopped},
	}

	machinesByID := make(map[string]*fly.Machine)
	for _, m := range machines {
		machinesByID[m.ID] = m
	}

	// Client operates on the in-memory list of machines above.
	var client mock.FlyClient
	client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
		mu.Lock()
		defer mu.Unlock()
		return machines, nil
	}
	client.StartFunc = func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
		mu.Lock()
		defer mu.Unlock()

		m := machinesByID[id]
		if m.State != fly.MachineStateStopped {
			return nil, fmt.Errorf("unexpected state: %q", m.State)
		}

		m.State = fly.MachineStateStarted
		return &fly.MachineStartResponse{}, nil
	}
	client.StopFunc = func(ctx context.Context, in fly.StopMachineInput, nonce string) error {
		mu.Lock()
		defer mu.Unlock()

		m := machinesByID[in.ID]
		if m.State != fly.MachineStateStarted {
			return fmt.Errorf("unexpected state: %q", m.State)
		}

		m.State = fly.MachineStateStopped
		return nil
	}

	// Collector will simply mirror the target value.
	var target atomic.Int64
	collector := mock.NewMetricCollector("target")
	collector.CollectMetricFunc = func(ctx context.Context) (float64, error) {
		return float64(target.Load()), nil
	}

	r := fas.NewReconciler(&client)
	r.Interval = 100 * time.Millisecond
	r.MinStartedMachineN = "target"
	r.MaxStartedMachineN = r.MinStartedMachineN
	r.Collectors = []fas.MetricCollector{collector}
	r.Start()
	defer r.Stop()

	waitInterval := 5 * r.Interval

	// Ensure no machines are started.
	time.Sleep(waitInterval)
	if got, want := machineCountByState(machines, fly.MachineStateStarted), 0; got != want {
		t.Fatalf("started=%v, want %v", got, want)
	}

	t.Log("Increase target count...")
	target.Store(2)
	time.Sleep(waitInterval)
	if got, want := machineCountByState(machines, fly.MachineStateStarted), 2; got != want {
		t.Fatalf("started=%v, want %v", got, want)
	}

	t.Log("Increase target count to max...")
	target.Store(4)
	time.Sleep(waitInterval)
	if got, want := machineCountByState(machines, fly.MachineStateStarted), 4; got != want {
		t.Fatalf("started=%v, want %v", got, want)
	}

	t.Log("Exceed total machine count...")
	target.Store(10)
	time.Sleep(waitInterval)
	if got, want := machineCountByState(machines, fly.MachineStateStarted), 4; got != want {
		t.Fatalf("started=%v, want %v", got, want)
	}

	t.Log("Downscale to zero...")
	target.Store(0)
	time.Sleep(waitInterval)
	if got, want := machineCountByState(machines, fly.MachineStateStarted), 0; got != want {
		t.Fatalf("started=%v, want %v", got, want)
	}

	t.Log("Test complete")
}

func machineCountByState(a []*fly.Machine, state string) (n int) {
	for _, m := range a {
		if m.State == state {
			n++
		}
	}
	return n
}
