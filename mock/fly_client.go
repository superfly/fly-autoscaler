package mock

import (
	"context"

	fas "github.com/superfly/fly-autoscaler"
	"github.com/superfly/fly-go"
)

var _ fas.FlyClient = (*FlyClient)(nil)

type FlyClient struct {
	GetOrganizationBySlugFunc        func(ctx context.Context, slug string) (*fly.Organization, error)
	GetAppsForOrganizationFunc       func(ctx context.Context, orgID string) ([]fly.App, error)
	GetAppCurrentReleaseMachinesFunc func(ctx context.Context, appName string) (*fly.Release, error)
}

func (m *FlyClient) GetOrganizationBySlug(ctx context.Context, slug string) (*fly.Organization, error) {
	return m.GetOrganizationBySlugFunc(ctx, slug)
}

func (m *FlyClient) GetAppsForOrganization(ctx context.Context, orgID string) ([]fly.App, error) {
	return m.GetAppsForOrganizationFunc(ctx, orgID)
}

func (m *FlyClient) GetAppCurrentReleaseMachines(ctx context.Context, appName string) (*fly.Release, error) {
	return m.GetAppCurrentReleaseMachinesFunc(ctx, appName)
}
