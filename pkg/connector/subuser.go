package connector

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
)

type subuserBuilder struct {
	resourceType   *v2.ResourceType
	client         client.SendGridClient
	ignoreSubusers bool
}

func newSubuserBuilder(c client.SendGridClient, ignoreSubusers bool) *subuserBuilder {
	return &subuserBuilder{
		resourceType:   subuserResourceType,
		client:         c,
		ignoreSubusers: ignoreSubusers,
	}
}

func (r *subuserBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subuserResourceType
}

func (r *subuserBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	var rv []*v2.Resource

	if r.ignoreSubusers {
		return rv, "", nil, nil
	}

	subusers, pNextToken, err := r.client.GetSubusers(ctx, pToken)
	if err != nil {
		return nil, "", nil, err
	}

	for _, subuser := range subusers {
		rb, err := subuserResource(ctx, subuser, nil)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, rb)
	}

	nextToken := ""
	if len(subusers) != 0 {
		nextToken = pNextToken
	}

	return rv, nextToken, nil, nil
}

func (r *subuserBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (r *subuserBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}
