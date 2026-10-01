package prometheus_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superfly/fly-autoscaler/prometheus"
)

// Ensure CollectMetric returns once the caller's context expires, even if the
// Prometheus server accepts the connection and never responds. Otherwise the
// reconciler blocks forever on a single hung query.
func TestMetricCollector_CollectMetric_ContextTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	c, err := prometheus.NewMetricCollector("foo", srv.URL, "sum(bar)", "")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.CollectMetric(ctx, "myapp")
		errCh <- err
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CollectMetric did not return after context deadline")
	}
}
