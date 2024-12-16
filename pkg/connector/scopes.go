package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type scopeBuilder struct {
	resourceType *v2.ResourceType
	client       client.SendGridClient
	scopeCache   *scopeCache
}

const (
	assignedEntitlement = "assigned"
)

func (r *scopeBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return scopeResourceType
}

func (r *scopeBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	if pToken == nil || pToken.Token == "" {
		err := r.scopeCache.buildCache(ctx)
		if err != nil {
			return nil, "", nil, err
		}
	}

	rv := make([]*v2.Resource, len(SendGridScopes))

	for i, scope := range SendGridScopes {
		rb, err := scopeResource(ctx, scope, nil)
		if err != nil {
			return nil, "", nil, err
		}

		rv[i] = rb
	}

	return rv, "", nil, nil
}

func (r *scopeBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(userResourceType),
		ent.WithDescription(fmt.Sprintf("Assigned %s to scopes", userResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s scope %s", userResourceType.DisplayName, resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (r *scopeBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	scope := resource.Id.Resource

	users := r.scopeCache.GetUsersForScope(scope)

	var rv []*v2.Grant

	for _, user := range users {
		userGrants, err := createGrantToUserFromTeammateScope(ctx, resource, user)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, userGrants...)
	}

	return rv, "", nil, nil
}

func newScopeBuilder(c client.SendGridClient, cache *scopeCache) *scopeBuilder {
	return &scopeBuilder{
		resourceType: scopeResourceType,
		client:       c,
		scopeCache:   cache,
	}
}

func createGrantToUserFromTeammateScope(ctx context.Context, resource *v2.Resource, user *client.TeammateScope) ([]*v2.Grant, error) {
	var rv []*v2.Grant
	l := ctxzap.Extract(ctx)

	for _, scope := range user.Scopes {
		if scope == "" {
			l.Warn("empty scope", zap.String("scope", scope))
			continue
		}

		userR, err := userResource(ctx, &user.Teammate, nil)
		if err != nil {
			return nil, err
		}

		grantToUser := grant.NewGrant(resource, assignedEntitlement, userR.Id)
		rv = append(rv, grantToUser)
	}

	return rv, nil
}
