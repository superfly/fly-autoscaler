package mock

import (
	"context"

	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-go"
)

var _ fas.FlyClient = (*FlyClient)(nil)

type FlyClient struct {
	ListFunc  func(ctx context.Context, state string) ([]*fly.Machine, error)
	StartFunc func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
}

func (c *FlyClient) List(ctx context.Context, state string) ([]*fly.Machine, error) {
	return c.ListFunc(ctx, state)
}

func (c *FlyClient) Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
	return c.StartFunc(ctx, id, nonce)
}
