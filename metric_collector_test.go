package fas

import (
	"context"
	"testing"
)

func TestExpandMetricQuery(t *testing.T) {
	t.Run("Static", func(t *testing.T) {
		result := ExpandMetricQuery(context.Background(), "foo", "my-app")
		if got, want := result, `foo`; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("Bare", func(t *testing.T) {
		result := ExpandMetricQuery(context.Background(), "foo $APP_NAME bar", "my-app")
		if got, want := result, `foo my-app bar`; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("Wrapped", func(t *testing.T) {
		result := ExpandMetricQuery(context.Background(), "foo${APP_NAME}bar", "my-app")
		if got, want := result, `foomy-appbar`; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}
