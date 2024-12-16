package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
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

	subusers, err := r.client.GetSubusers(ctx)
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

	return rv, "", nil, nil
}

func (r *subuserBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(userResourceType),
		ent.WithDescription(fmt.Sprintf("Assigned %s to subusers", userResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s subuser %s", userResourceType.DisplayName, resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (r *subuserBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	subuserEmail := resource.Id.Resource

	var rv []*v2.Grant
	if r.ignoreSubusers {
		return rv, "", nil, nil
	}

	userGrants, err := createGrantToUserFromSubuser(ctx, resource, subuserEmail)
	if err != nil {
		return nil, "", nil, err
	}

	rv = append(rv, userGrants...)

	return rv, "", nil, nil
}

func createGrantToUserFromSubuser(ctx context.Context, resource *v2.Resource, email string) ([]*v2.Grant, error) {
	userId, err := rs.NewResourceID(userResourceType, email)
	if err != nil {
		return nil, err
	}

	rv := []*v2.Grant{
		grant.NewGrant(resource, assignedEntitlement, userId),
	}

	return rv, nil
}
