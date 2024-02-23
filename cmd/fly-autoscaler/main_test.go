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
