package main_test

import (
	"testing"
	"time"

	main "github.com/superfly/fly-autoscaler/cmd/fly-autoscaler"
)

func TestConfig_Parse(t *testing.T) {
	var config main.Config
	if err := main.ParseConfigFromFile("../../etc/fly-autoscaler.yml", &config); err != nil {
		t.Fatal(err)
	}

	if got, want := config.AppName, "TARGET_APP_NAME"; got != want {
		t.Fatalf("AppName=%v, want %v", got, want)
	}
	if got, want := config.StartedMachineN, "ceil(queue_depth / 10)"; got != want {
		t.Fatalf("StartedMachineN=%v, want %v", got, want)
	}
	if got, want := config.Interval, 15*time.Second; got != want {
		t.Fatalf("Interval=%v, want %v", got, want)
	}
	if got, want := config.APIToken, "FlyV1 ..."; got != want {
		t.Fatalf("APIToken=%v, want %v", got, want)
	}
	if got, want := config.Verbose, false; got != want {
		t.Fatalf("Verbose=%v, want %v", got, want)
	}
	if got, want := config.ProcessGroup, "app"; got != want {
		t.Fatalf("ProcessGroup=%v, want %v", got, want)
	}

	mc := config.MetricCollectors[0]
	if got, want := mc.Type, "prometheus"; got != want {
		t.Fatalf("MC[0].Type=%v, want %v", got, want)
	}
	if got, want := mc.MetricName, "queue_depth"; got != want {
		t.Fatalf("MC[0].MetricName=%v, want %v", got, want)
	}
	if got, want := mc.Address, "https://api.fly.io/prometheus/MY_ORG"; got != want {
		t.Fatalf("MC[0].Address=%v, want %v", got, want)
	}
	if got, want := mc.Query, "sum(queue_depth)"; got != want {
		t.Fatalf("MC[0].Query=%v, want %v", got, want)
	}
	if got, want := mc.Token, "FlyV1 ..."; got != want {
		t.Fatalf("MC[0].Token=%v, want %v", got, want)
	}
}

func TestConfig_Validate(t *testing.T) {
	t.Run("CreatedOrStartedMachineCount", func(t *testing.T) {
		c := &main.Config{AppName: "myapp"}
		if err := c.Validate(); err == nil || err.Error() != `must define either created machine count or started machine count` {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreatedMachineCount", func(t *testing.T) {
		t.Run("TooManyDefined", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", CreatedMachineN: "1", MinCreatedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `cannot define created machine count and min/max created machine count` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("MinNotMax", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", MinCreatedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `max created machine count required if min created machine count is defined` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("MaxNotMin", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", MaxCreatedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `min created machine count required if max created machine count is defined` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	})

	t.Run("StartedMachineCount", func(t *testing.T) {
		t.Run("TooManyDefined", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", StartedMachineN: "1", MinStartedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `cannot define started machine count and min/max started machine count` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("MinNotMax", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", MinStartedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `max started machine count required if min started machine count is defined` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
		t.Run("MaxNotMin", func(t *testing.T) {
			c := &main.Config{AppName: "myapp", MaxStartedMachineN: "1"}
			if err := c.Validate(); err == nil || err.Error() != `min started machine count required if max started machine count is defined` {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	})
}
