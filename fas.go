package fas

import (
	"context"
	"errors"

	fly "github.com/superfly/fly-go"
)

// Expression errors.
var (
	ErrExprNaN = errors.New("expression returned NaN")
	ErrExprInf = errors.New("expression returned Inf")
)

type FlyClient interface {
	List(ctx context.Context, state string) ([]*fly.Machine, error)
	Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
}
