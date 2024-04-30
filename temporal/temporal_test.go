//go:build integration

package temporal_test

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/superfly/fly-autoscaler/temporal"
)

func TestMetricCollector_CollectMetric(t *testing.T) {
	runTemporalDevServer(t)

	c := temporal.NewMetricCollector("foo")
	c.Address = "localhost:7233"
	c.Namespace = "default"
	// c.Query = `WorkflowType="workflow1"`
	if err := c.Open(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	// Start some workflow executions.
	t.Log("starting workflow executions")
	if _, err := exec.Command("temporal", "workflow", "start", "--type", "workflow1", "--task-queue", "queue1").Output(); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.Command("temporal", "workflow", "start", "--type", "workflow1", "--task-queue", "queue1").Output(); err != nil {
		t.Fatal(err)
	}

	t.Log("querying metric")
	if v, err := c.CollectMetric(context.Background(), "myapp"); err != nil {
		t.Fatal(err)
	} else if got, want := v, 2.0; got != want {
		t.Fatalf("metric=%v, want %v", got, want)
	}

	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func runTemporalDevServer(tb testing.TB) {
	cmd := exec.Command("temporal", "server", "start-dev")
	if testing.Verbose() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Start(); err != nil {
		tb.Fatalf("failed to start temporal dev server: %s", err)
	}

	time.Sleep(1 * time.Second)

	tb.Cleanup(func() {
		if err := cmd.Process.Kill(); err != nil {
			tb.Logf("failed to kill temporal dev server: %s", err)
		}
		_ = cmd.Wait()
	})
}
