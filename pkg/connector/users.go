package connector

import (
	"context"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
)

type userBuilder struct {
	client client.SendGridClient
}

func (u *userBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return userResourceType
}

// List returns all the users from the database as resource objects.
// Users include a UserTrait because they are the 'shape' of a standard user.
func (u *userBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	teammates, err := u.client.GetTeammates(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	rv := make([]*v2.Resource, len(teammates))
	for i, teammate := range teammates {
		us, err := userResource(ctx, &teammate, nil)
		if err != nil {
			return nil, "", nil, err
		}
		rv[i] = us
	}

	subusers, err := u.client.GetSubusers(ctx)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subuser := range subusers {
		us, err := userResourceFromSubuser(ctx, &subuser, nil)
		if err != nil {
			return nil, "", nil, err
		}
		rv = append(rv, us)
	}

	return rv, "", nil, nil
}

// Entitlements always returns an empty slice for users.
func (u *userBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

// Grants always returns an empty slice for users since they don't have any entitlements.
func (us *userBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func newUserBuilder(client client.SendGridClient) *userBuilder {
	return &userBuilder{
		client: client,
	}
}
