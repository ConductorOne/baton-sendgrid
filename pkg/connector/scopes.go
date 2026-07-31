package connector

import (
	"context"
	"fmt"
	"slices"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type scopeBuilder struct {
	resourceType *v2.ResourceType
	client       SendGridClient
}

const (
	assignedEntitlement = "assigned"
)

func (r *scopeBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return scopeResourceType
}

func (r *scopeBuilder) List(_ context.Context, _ *v2.ResourceId, _ rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	rv := make([]*v2.Resource, 0, len(SendGridScopes))

	for scope := range SendGridScopes {
		rb, err := scopeResource(scope)
		if err != nil {
			return nil, nil, err
		}

		rv = append(rv, rb)
	}

	return rv, &rs.SyncOpResults{}, nil
}

func (r *scopeBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	var rv []*v2.Entitlement
	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(teammateResourceType),
		ent.WithDescription(fmt.Sprintf("Assigned %s to scopes", teammateResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s scope %s", teammateResourceType.DisplayName, resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, &rs.SyncOpResults{}, nil
}

// Grants returns empty — scope grants are emitted by teammateBuilder.Grants.
func (r *scopeBuilder) Grants(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}

// ResourceProvisioner

func (r *scopeBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != teammateResourceType.Id {
		return nil, nil, fmt.Errorf("baton-sendgrid: principal resource type is not %s", teammateResourceType.Id)
	}

	scopeId := entitlement.Resource.Id.Resource
	principalUsername := principal.Id.Resource

	onBehalfOf, err := teammateOnBehalfOf(ctx, r.client, principal)
	if err != nil {
		return nil, nil, err
	}

	teammate, onBehalfOf, err := getTeammateWithFreshOnBehalfOf(ctx, r.client, principal, principalUsername, onBehalfOf)
	if err != nil {
		return nil, nil, err
	}

	index := slices.IndexFunc(teammate.Scopes, func(c string) bool {
		return c == scopeId
	})
	if index >= 0 {
		l.Info(
			"baton-sendgrid: scope already granted to teammate",
			zap.String("scope", scopeId),
			zap.String("teammate", principalUsername),
		)

		return []*v2.Grant{}, annotations.New(&v2.GrantAlreadyExists{}), nil
	}

	teammate.Scopes = append(teammate.Scopes, scopeId)

	err = r.client.SetTeammateScopes(ctx, principalUsername, teammate.Scopes, teammate.IsAdmin, onBehalfOf)
	if err != nil {
		return nil, nil, err
	}

	scopeRs, err := scopeResource(Scope(scopeId))
	if err != nil {
		return nil, nil, err
	}

	return []*v2.Grant{grant.NewGrant(scopeRs, assignedEntitlement, principal.Id)}, nil, nil
}

func (r *scopeBuilder) Revoke(ctx context.Context, g *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	principal := g.Principal
	scopeToRemove := g.Entitlement.Resource.Id.Resource
	principalUsername := principal.Id.Resource

	if principal.Id.ResourceType != teammateResourceType.Id {
		return nil, fmt.Errorf("baton-sendgrid: principal resource type is not %s", teammateResourceType.Id)
	}

	onBehalfOf, err := teammateOnBehalfOf(ctx, r.client, principal)
	if err != nil {
		return nil, err
	}

	teammate, onBehalfOf, err := getTeammateWithFreshOnBehalfOf(ctx, r.client, principal, principalUsername, onBehalfOf)
	if err != nil {
		return nil, err
	}

	index := slices.IndexFunc(teammate.Scopes, func(c string) bool {
		return c == scopeToRemove
	})
	if index < 0 {
		l.Info(
			"baton-sendgrid: scope not found in teammate",
			zap.String("scope", scopeToRemove),
			zap.String("teammate", principalUsername),
		)

		return annotations.New(&v2.GrantAlreadyRevoked{}), nil
	}

	teammate.Scopes = append(teammate.Scopes[:index], teammate.Scopes[index+1:]...)

	err = r.client.SetTeammateScopes(ctx, principalUsername, teammate.Scopes, teammate.IsAdmin, onBehalfOf)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func newScopeBuilder(c SendGridClient) *scopeBuilder {
	return &scopeBuilder{
		resourceType: scopeResourceType,
		client:       c,
	}
}
