package fas_test

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"testing"

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
		r := fas.NewReconciler()
		r.SetValue("foo", 100)
		if v, ok := r.Value("foo"); !ok {
			t.Fatal("expected value")
		} else if got, want := v, 100.0; got != want {
			t.Fatalf("foo=%v, want %v", got, want)
		}
	})

	t.Run("NoValue", func(t *testing.T) {
		r := fas.NewReconciler()
		if _, ok := r.Value("foo"); ok {
			t.Fatal("expected no value")
		}
	})
}

func TestReconciler_MinStartedMachineN(t *testing.T) {
	t.Run("Constant", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "1"
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 1; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Round", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "2.6"
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 3; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Var", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "x + y + 2"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 13; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Min", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "min(x, y)"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 4; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Max", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "max(x, y)"
		r.SetValue("x", 4)
		r.SetValue("y", 7)
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 7; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Neg", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "-2"
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if !ok {
			t.Fatal("expected ok")
		} else if got, want := v, 0; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("Blank", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = ""
		if v, ok, err := r.CalcMinStartedMachineN(); err != nil {
			t.Fatal(err)
		} else if ok {
			t.Fatal("expected not ok")
		} else if got, want := v, 0; got != want {
			t.Fatalf("MinStartedMachineN=%v, want %v", got, want)
		}
	})

	t.Run("NaN", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "x + 1"
		r.SetValue("x", math.NaN())
		if _, _, err := r.CalcMinStartedMachineN(); err == nil || err != fas.ErrExprNaN {
			t.Fatal(err)
		}
	})

	t.Run("Inf", func(t *testing.T) {
		r := fas.NewReconciler()
		r.MinStartedMachineN = "1 / 0"
		if _, _, err := r.CalcMinStartedMachineN(); err == nil || err != fas.ErrExprInf {
			t.Fatal(err)
		}
	})
}

// Ensure that if the target count and started count are the same, there
// will not be any new machines started.
func TestReconciler_Scale_NoScale(t *testing.T) {
	var client mock.FlapsClient
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

	r := fas.NewReconciler()
	r.Client = &client
	r.MinStartedMachineN = "1"
	r.MaxStartedMachineN = "2"
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	} else if got, want := r.Stats.NoScale.Load(), int64(1); got != want {
		t.Fatalf("NoScale=%v, want %v", got, want)
	}
}

func TestReconciler_Scale_Create(t *testing.T) {
	// Ensure that machines will be created when below the min number.
	t.Run("OK", func(t *testing.T) {
		var invokeCreateN int
		var client mock.FlapsClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{
					ID:     "1",
					State:  fly.MachineStateStarted,
					Region: "iad",
					Config: &fly.MachineConfig{
						Metadata: map[string]string{"foo": "bar"},
					},
				},
				{
					ID:     "2",
					State:  fly.MachineStateStopped,
					Region: "den",
					Config: &fly.MachineConfig{
						Metadata: map[string]string{"baz": "bat"},
					},
				},
			}, nil
		}
		client.LaunchFunc = func(ctx context.Context, input fly.LaunchMachineInput) (*fly.Machine, error) {
			invokeCreateN++

			// Ensure we are using the first machine's metadata.
			if got, want := input.Config.Metadata["foo"], "bar"; got != want {
				t.Fatalf("metadata=%v, want %v", got, want)
			}

			switch invokeCreateN {
			case 1:
				return &fly.Machine{ID: "1", Region: input.Region}, nil
			case 2:
				return &fly.Machine{ID: "2", Region: input.Region}, nil
			default:
				t.Fatalf("too many 'launch' invocations")
				return nil, nil
			}
		}

		r := fas.NewReconciler()
		r.Client = &client
		r.MinCreatedMachineN, r.MaxCreatedMachineN = "4", "4"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeCreateN, 2; got != want {
			t.Fatalf("createN=%v, want %v", got, want)
		}
		if got, want := r.Stats.BulkCreate.Load(), int64(1); got != want {
			t.Fatalf("BulkCreate=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineCreated.Load(), int64(2); got != want {
			t.Fatalf("MachineCreated=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineCreateFailed.Load(), int64(0); got != want {
			t.Fatalf("MachineCreateFailed=%v, want %v", got, want)
		}
	})

	// Ensure that an error occurs when creating a machine with no machine to clone.
	t.Run("ErrNoMachineAvailable", func(t *testing.T) {
		var client mock.FlapsClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{}, nil
		}
		client.LaunchFunc = func(ctx context.Context, input fly.LaunchMachineInput) (*fly.Machine, error) {
			return nil, fmt.Errorf("unexpected launch invocation")
		}

		r := fas.NewReconciler()
		r.Client = &client
		r.MinCreatedMachineN = "1"
		if err := r.Reconcile(context.Background()); err == nil || err.Error() != `no machine available to clone for scale up` {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// Ensure that machines will be destroyed when above the max count.
func TestReconciler_Scale_Destroy(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		var invokeDestroyN int
		var client mock.FlapsClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStarted, Region: "iad"},
				{ID: "2", State: fly.MachineStateStopped, Region: "den"},
				{ID: "3", State: fly.MachineStateStopped, Region: "iad"},
				{ID: "4", State: fly.MachineStateStopped, Region: "mad"},
			}, nil
		}
		client.DestroyFunc = func(ctx context.Context, input fly.RemoveMachineInput, nonce string) error {
			invokeDestroyN++
			switch input.ID {
			case "2", "3":
				// ok
			default:
				t.Fatalf("unexpected machine id: %q", input.ID)
			}
			return nil
		}

		r := fas.NewReconciler()
		r.Client = &client
		r.MinCreatedMachineN, r.MaxCreatedMachineN = "2", "2"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeDestroyN, 2; got != want {
			t.Fatalf("destroyN=%v, want %v", got, want)
		}
		if got, want := r.Stats.BulkDestroy.Load(), int64(1); got != want {
			t.Fatalf("BulkDestroy=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineDestroyed.Load(), int64(2); got != want {
			t.Fatalf("MachineDestroyed=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineDestroyFailed.Load(), int64(0); got != want {
			t.Fatalf("MachineDestroyFailed=%v, want %v", got, want)
		}
	})

	// Ensure we always leave at least 1 machine available so we can clone for scale up.
	t.Run("AttemptScaleToZero", func(t *testing.T) {
		var client mock.FlapsClient
		client.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
			return []*fly.Machine{
				{ID: "1", State: fly.MachineStateStopped, Region: "iad"},
				{ID: "2", State: fly.MachineStateStopped, Region: "iad"},
				{ID: "3", State: fly.MachineStateStarted, Region: "iad"},
				{ID: "4", State: fly.MachineStateCreated, Region: "iad"},
			}, nil
		}
		client.DestroyFunc = func(ctx context.Context, input fly.RemoveMachineInput, nonce string) error {
			return nil
		}

		r := fas.NewReconciler()
		r.Client = &client
		r.MaxCreatedMachineN = "0"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got, want := r.Stats.MachineDestroyed.Load(), int64(3); got != want {
			t.Fatalf("MachineDestroyed=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineDestroyFailed.Load(), int64(0); got != want {
			t.Fatalf("MachineDestroyFailed=%v, want %v", got, want)
		}
	})
}

// Ensure that number of machines will be scaled up to match target number.
func TestReconciler_Scale_Start(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		var invokeStartN int
		var client mock.FlapsClient
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

		r := fas.NewReconciler()
		r.Client = &client
		r.MinStartedMachineN = "foo + 2"
		r.MaxStartedMachineN = r.MinStartedMachineN
		r.SetValue("foo", 1.0)
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeStartN, 2; got != want {
			t.Fatalf("startN=%v, want %v", got, want)
		}
		if got, want := r.Stats.BulkStart.Load(), int64(1); got != want {
			t.Fatalf("BulkStart=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStarted.Load(), int64(2); got != want {
			t.Fatalf("MachineStarted=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStartFailed.Load(), int64(0); got != want {
			t.Fatalf("MachineStartFailed=%v, want %v", got, want)
		}
	})

	// Ensure that the reconciler will keep trying to start machines if one fails.
	t.Run("Failed", func(t *testing.T) {
		var invokeStartN int
		var client mock.FlapsClient
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

		r := fas.NewReconciler()
		r.Client = &client
		r.MinStartedMachineN = "2"
		r.MaxStartedMachineN = r.MinStartedMachineN
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := invokeStartN, 3; got != want {
			t.Fatalf("startN=%v, want %v", got, want)
		} else if got, want := r.Stats.BulkStart.Load(), int64(1); got != want {
			t.Fatalf("BulkStart=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStarted.Load(), int64(2); got != want {
			t.Fatalf("MachineStarted=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStartFailed.Load(), int64(1); got != want {
			t.Fatalf("MachineStartFailed=%v, want %v", got, want)
		}
	})
}

// Ensure the reconciler should stop machines when they are above the max count.
func TestReconciler_Scale_Stop(t *testing.T) {
	t.Run("OK", func(t *testing.T) {
		var client mock.FlapsClient
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

		r := fas.NewReconciler()
		r.Client = &client
		r.MinStartedMachineN = "1"
		r.MaxStartedMachineN = "1"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := r.Stats.BulkStop.Load(), int64(1); got != want {
			t.Fatalf("BulkStop=%v, want %v", got, want)
		} else if got, want := r.Stats.MachineStopped.Load(), int64(2); got != want {
			t.Fatalf("MachineStopped=%v, want %v", got, want)
		}
	})

	t.Run("Failed", func(t *testing.T) {
		var client mock.FlapsClient
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

		r := fas.NewReconciler()
		r.Client = &client
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

func machineCountByState(a []*fly.Machine, state string) (n int) {
	for _, m := range a {
		if m.State == state {
			n++
		}
	}
	return n
}
