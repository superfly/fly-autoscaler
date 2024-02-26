package fas

import (
	"context"
	"errors"

	fly "github.com/superfly/fly-go"
)

// Expression errors.
var (
	ErrExprRequired = errors.New("expression required")
	ErrExprNaN      = errors.New("expression returned NaN")
	ErrExprInf      = errors.New("expression returned Inf")
)

type FlyClient interface {
	List(ctx context.Context, state string) ([]*fly.Machine, error)
	Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
	Stop(ctx context.Context, in fly.StopMachineInput, nonce string) error
}
