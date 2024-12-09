package mock

import (
	"context"

	"github.com/superfly/fly-go"
)

type FlapsClient struct {
	ListFunc    func(ctx context.Context, state string) ([]*fly.Machine, error)
	LaunchFunc  func(ctx context.Context, input fly.LaunchMachineInput) (*fly.Machine, error)
	DestroyFunc func(ctx context.Context, input fly.RemoveMachineInput, nonce string) error
	StartFunc   func(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error)
	StopFunc    func(ctx context.Context, in fly.StopMachineInput, nonce string) error
}

func (c *FlapsClient) List(ctx context.Context, state string) ([]*fly.Machine, error) {
	return c.ListFunc(ctx, state)
}

func (c *FlapsClient) Launch(ctx context.Context, config fly.LaunchMachineInput) (*fly.Machine, error) {
	return c.LaunchFunc(ctx, config)
}

func (c *FlapsClient) Destroy(ctx context.Context, input fly.RemoveMachineInput, nonce string) error {
	return c.DestroyFunc(ctx, input, nonce)
}

func (c *FlapsClient) Start(ctx context.Context, id, nonce string) (*fly.MachineStartResponse, error) {
	return c.StartFunc(ctx, id, nonce)
}

func (c *FlapsClient) Stop(ctx context.Context, in fly.StopMachineInput, nonce string) error {
	return c.StopFunc(ctx, in, nonce)
}
