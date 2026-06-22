package connector

import (
	"context"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

type subuserBuilder struct {
	resourceType   *v2.ResourceType
	client         SendGridClient
	ignoreSubusers bool
}

func newSubuserBuilder(c SendGridClient, ignoreSubusers bool) *subuserBuilder {
	return &subuserBuilder{
		resourceType:   subuserResourceType,
		client:         c,
		ignoreSubusers: ignoreSubusers,
	}
}

func (r *subuserBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subuserResourceType
}

func (r *subuserBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	var rv []*v2.Resource

	if r.ignoreSubusers {
		return rv, &rs.SyncOpResults{}, nil
	}

	subusers, pNextToken, err := r.client.GetSubusers(ctx, &opts.PageToken)
	if err != nil {
		return nil, nil, err
	}

	for _, subuser := range subusers {
		rb, err := subuserResource(subuser)
		if err != nil {
			return nil, nil, err
		}

		rv = append(rv, rb)
	}

	nextToken := ""
	if len(subusers) != 0 {
		nextToken = pNextToken
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextToken}, nil
}

func (r *subuserBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}

func (r *subuserBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}
