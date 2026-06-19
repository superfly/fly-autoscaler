package fas_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-autoscaler/mock"
	fly "github.com/superfly/fly-go"
)

func TestFormatWildcardAsRegexp(t *testing.T) {
	for _, tt := range []struct {
		in, out string
	}{
		{"", ".*"},                        // match all
		{"*", "^.*$"},                     // match all
		{"my-app", "^my-app$"},            // exact match
		{"my-app-*", "^my-app-.*$"},       // suffix match
		{"my-*-app", "^my-.*-app$"},       // infix match
		{"*-my-app", "^.*-my-app$"},       // prefix match
		{"my-[app]*", "^my-\\[app\\].*$"}, // escaped characters
	} {
		t.Run("", func(t *testing.T) {
			if got, want := fas.FormatWildcardAsRegexp(tt.in), tt.out; got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// Ensure prometheus registration does not blow up.
func TestReconcilerPool_RegisterPromMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	fas.NewReconcilerPool(nil, 1).RegisterPromMetrics(reg)
	if _, err := reg.Gather(); err != nil {
		t.Fatal(err)
	}
}

func TestReconcilerPool_Run_SingleApp(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode enabled, skipping")
	}

	var mu sync.Mutex
	machines := []*fly.Machine{
		{ID: "1", State: fly.MachineStateStopped, HostStatus: fly.HostStatusOk},
		{ID: "2", State: fly.MachineStateStopped, HostStatus: fly.HostStatusOk},
		{ID: "3", State: fly.MachineStateStopped, HostStatus: fly.HostStatusOk},
		{ID: "4", State: fly.MachineStateStopped, HostStatus: fly.HostStatusOk},
	}

	machinesByID := make(map[string]*fly.Machine)
	for _, m := range machines {
		machinesByID[m.ID] = m
	}

	var flyClient mock.FlyClient
	flyClient.GetOrganizationBySlugFunc = func(ctx context.Context, slug string) (*fly.Organization, error) {
		if got, want := slug, "myorg"; got != want {
			t.Fatalf("slug=%q, want %q", got, want)
		}
		return &fly.Organization{ID: "123"}, nil
	}
	flyClient.GetAppsForOrganizationFunc = func(ctx context.Context, orgID string) ([]fly.App, error) {
		if got, want := orgID, "123"; got != want {
			t.Fatalf("id=%q, want %q", got, want)
		}
		return []fly.App{
			{Name: "other-app"},
			{Name: "my-app-1"},
		}, nil
	}
	flyClient.GetAppCurrentReleaseMachinesFunc = func(ctx context.Context, appName string) (*fly.Release, error) {
		return &fly.Release{InProgress: false, Status: "completed"}, nil
	}

	// Client operates on the in-memory list of machines above.
	var flapsClient mock.FlapsClient
	flapsClient.ListFunc = func(ctx context.Context, state string) ([]*fly.Machine, error) {
		mu.Lock()
		defer mu.Unlock()
		return machines, nil
	}
	flapsClient.StartFunc = func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
		mu.Lock()
		defer mu.Unlock()

		m := machinesByID[id]
		if m.State != fly.MachineStateStopped {
			return nil, fmt.Errorf("unexpected state: %q", m.State)
		}

		m.State = fly.MachineStateStarted
		return &fly.MachineStartResponse{}, nil
	}
	flapsClient.StopFunc = func(ctx context.Context, in fly.StopMachineInput, nonce string) error {
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
	collector.CollectMetricFunc = func(ctx context.Context, app string) (float64, error) {
		return float64(target.Load()), nil
	}

	p := fas.NewReconcilerPool(&flyClient, 1)
	p.OrganizationSlug = "myorg"
	p.AppName = "my-app-*"
	p.ReconcileInterval = 100 * time.Millisecond
	p.NewReconciler = func() *fas.Reconciler {
		r := fas.NewReconciler()
		r.Client = &flapsClient
		r.MinStartedMachineN = "target"
		r.MaxStartedMachineN = r.MinStartedMachineN
		r.Collectors = []fas.MetricCollector{collector}
		return r
	}
	p.NewFlapsClient = func(ctx context.Context, name string) (fas.FlapsClient, error) {
		return &flapsClient, nil
	}
	if err := p.Open(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	waitInterval := 5 * p.ReconcileInterval

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

	t.Log("Closing pool")
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}

	t.Log("Test complete")
}
