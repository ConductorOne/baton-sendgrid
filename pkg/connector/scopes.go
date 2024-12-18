package connector

import (
	"context"
	"fmt"
	"slices"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"

	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
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
		ent.WithGrantableTo(teammateResourceType),
		ent.WithDescription(fmt.Sprintf("Assigned %s to scopes", teammateResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s scope %s", teammateResourceType.DisplayName, resource.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, assignedEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (r *scopeBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	scope := resource.Id.Resource

	users := r.scopeCache.GetUsersForScope(scope)

	var rv []*v2.Grant

	for _, user := range users {
		userGrants, err := createGrantToScopeFromTeammateScope(ctx, resource, user)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, userGrants...)
	}

	return rv, "", nil, nil
}

// ResourceProvisioner

func (r *scopeBuilder) Grant(ctx context.Context, principal *v2.Resource, entitlement *v2.Entitlement) ([]*v2.Grant, annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if principal.Id.ResourceType != teammateResourceType.Id {
		return nil, nil, fmt.Errorf("baton-sendgrid: principal resource type is not %s", teammateResourceType.Id)
	}

	scopeId := entitlement.Resource.Id.Resource
	principalUsername := principal.Id.Resource

	teammate, err := r.client.GetSpecificTeammate(ctx, principalUsername)
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

	err = r.client.SetTeammateScopes(ctx, principalUsername, teammate.Scopes, teammate.IsAdmin)
	if err != nil {
		return nil, nil, err
	}

	scopeRs, err := scopeResource(ctx, Scope(scopeId), nil)
	if err != nil {
		return nil, nil, err
	}

	grants, err := createGrantToScopeFromTeammateScope(ctx, scopeRs, teammate)
	if err != nil {
		return nil, nil, err
	}

	return grants, nil, nil
}

func (r *scopeBuilder) Revoke(ctx context.Context, grant *v2.Grant) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	principal := grant.Principal
	scopeToRemove := grant.Entitlement.Resource.Id.Resource

	principalUsername := principal.Id.Resource

	if principal.Id.ResourceType != teammateResourceType.Id {
		return nil, fmt.Errorf("baton-sendgrid: principal resource type is not %s", teammateResourceType.Id)
	}

	teammate, err := r.client.GetSpecificTeammate(ctx, principalUsername)
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

	var scopesToSend []string
	if index == 0 {
		scopesToSend = teammate.Scopes[1:]
	} else {
		scopesToSend = append(teammate.Scopes[:index], teammate.Scopes[index+1:]...)
	}

	err = r.client.SetTeammateScopes(ctx, principalUsername, scopesToSend, teammate.IsAdmin)
	if err != nil {
		return nil, err
	}

	return nil, nil
}

func newScopeBuilder(c client.SendGridClient, cache *scopeCache) *scopeBuilder {
	return &scopeBuilder{
		resourceType: scopeResourceType,
		client:       c,
		scopeCache:   cache,
	}
}

func createGrantToScopeFromTeammateScope(ctx context.Context, resource *v2.Resource, teammate *client.TeammateScope) ([]*v2.Grant, error) {
	var rv []*v2.Grant
	l := ctxzap.Extract(ctx)

	for _, scope := range teammate.Scopes {
		if scope == "" {
			l.Warn("empty scope", zap.String("scope", scope))
			continue
		}

		userR, err := teammateResource(ctx, &teammate.Teammate, nil)
		if err != nil {
			return nil, err
		}

		grantToUser := grant.NewGrant(resource, assignedEntitlement, userR.Id)
		rv = append(rv, grantToUser)
	}

	return rv, nil
}
