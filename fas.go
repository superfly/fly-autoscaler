package fas

import (
	"context"
	"errors"

	fly "github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
)

// Expression errors.
var (
	ErrExprRequired = errors.New("expression required")
	ErrExprNaN      = errors.New("expression returned NaN")
	ErrExprInf      = errors.New("expression returned Inf")
)

var _ FlapsClient = (*flaps.Client)(nil)

type FlapsClient interface {
	List(ctx context.Context, state string) ([]*fly.Machine, error)
	Launch(ctx context.Context, input fly.LaunchMachineInput) (*fly.Machine, error)
	Destroy(ctx context.Context, input fly.RemoveMachineInput, nonce string) error
	Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
	Stop(ctx context.Context, in fly.StopMachineInput, nonce string) error
}
