package mock

import (
	"context"

	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-go"
)

var _ fas.FlyClient = (*FlyClient)(nil)

type FlyClient struct {
	ListFunc    func(ctx context.Context, state string) ([]*fly.Machine, error)
	LaunchFunc  func(ctx context.Context, input fly.LaunchMachineInput) (*fly.Machine, error)
	DestroyFunc func(ctx context.Context, input fly.RemoveMachineInput, nonce string) error
	StartFunc   func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
	StopFunc    func(ctx context.Context, in fly.StopMachineInput, nonce string) error
}

func (c *FlyClient) List(ctx context.Context, state string) ([]*fly.Machine, error) {
	return c.ListFunc(ctx, state)
}

func (c *FlyClient) Launch(ctx context.Context, config fly.LaunchMachineInput) (*fly.Machine, error) {
	return c.LaunchFunc(ctx, config)
}

func (c *FlyClient) Destroy(ctx context.Context, input fly.RemoveMachineInput, nonce string) error {
	return c.DestroyFunc(ctx, input, nonce)
}

func (c *FlyClient) Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
	return c.StartFunc(ctx, id, nonce)
}

func (c *FlyClient) Stop(ctx context.Context, in fly.StopMachineInput, nonce string) error {
	return c.StopFunc(ctx, in, nonce)
}
