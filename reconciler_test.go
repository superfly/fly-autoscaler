package fas_test

import (
	"context"
	"log/slog"
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

func TestReconciler_Reconcile(t *testing.T) {
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
		r.Expr = "1"
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	// Ensure that number of machines will be scaled up to match target number.
	t.Run("ScaleUp", func(t *testing.T) {
		var startN int
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
				startN++
			default:
				t.Fatalf("unexpected start id: %v", id)
			}
			return &fly.MachineStartResponse{}, nil
		}

		r := fas.NewReconciler(&client)
		r.Expr = "foo + 2"
		r.SetValue("foo", 1.0)
		if err := r.Reconcile(context.Background()); err != nil {
			t.Fatal(err)
		} else if got, want := startN, 2; got != want {
			t.Fatalf("startN=%v, want %v", got, want)
		}
	})
}

/*
func TestReconciler_StartStop(t *testing.T) {
	var client mock.FlyClient
	collector := mock.NewMetricCollector("foo")

	r := fas.NewReconciler(&client)
	r.Expr = "foo + 1"
	r.Collectors = []fas.MetricCollector{collector}

	if v, err :=
}
*/
