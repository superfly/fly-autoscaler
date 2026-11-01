package prometheus_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superfly/fly-autoscaler/prometheus"
)

func TestCollectMetric_StalledBodyDeadlineAndRecovery(t *testing.T) {
	var calls atomic.Int32
	stalled := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if calls.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `{"status":"success","data":`)
			w.(http.Flusher).Flush()
			close(stalled)
			select {
			case <-r.Context().Done():
				close(canceled)
			case <-release:
			}
			return
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"6"]}]}}`)
	}))
	defer server.Close()
	defer close(release)
	collector, err := prometheus.NewMetricCollector("desired", server.URL, "desired", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := collector.CollectMetric(ctx, "example"); done <- err }()
	select {
	case <-stalled:
	case <-time.After(3 * time.Second):
		t.Fatal("request never reached stalled body")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want deadline exceeded, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("collection ignored caller deadline while reading body")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request was not canceled")
	}
	nextCtx, nextCancel := context.WithTimeout(context.Background(), time.Second)
	defer nextCancel()
	value, err := collector.CollectMetric(nextCtx, "example")
	if err != nil || value != 6 {
		t.Fatalf("subsequent collection = %v, %v; want 6, nil", value, err)
	}
}

func TestCollectMetric_CanceledContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"6"]}]}}`)
	}))
	defer server.Close()
	collector, err := prometheus.NewMetricCollector("desired", server.URL, "desired", "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = collector.CollectMetric(ctx, "example")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context canceled, got %v", err)
	}
}
