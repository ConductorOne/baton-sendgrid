package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	accessEntitlement = "access"
)

type teammateBuilder struct {
	client SendGridClient
}

func (u *teammateBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return teammateResourceType
}

func (u *teammateBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	teammates, pNextToken, err := u.client.GetTeammates(ctx, &opts.PageToken)
	if err != nil {
		return nil, nil, err
	}

	rv := make([]*v2.Resource, len(teammates))
	for i, teammate := range teammates {
		us, err := teammateResource(&teammate)
		if err != nil {
			return nil, nil, err
		}
		rv[i] = us
	}

	nextToken := ""
	if len(teammates) != 0 {
		nextToken = pNextToken
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextToken}, nil
}

func (u *teammateBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	var rv []*v2.Entitlement
	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(subuserResourceType),
		ent.WithDescription(fmt.Sprintf("Teammate acess to %s", subuserResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s can access %s", resource.DisplayName, subuserResourceType.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, accessEntitlement, assigmentOptions...))

	return rv, &rs.SyncOpResults{}, nil
}

func (u *teammateBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	var rv []*v2.Grant

	username := resource.Id.Resource

	// Subuser access grants.
	access, nextToken, err := u.client.GetTeammatesSubAccess(ctx, username, &opts.PageToken)
	if err != nil {
		return nil, nil, err
	}

	logger := ctxzap.Extract(ctx)
	logger.Info("Teammate grants", zap.String("username", username), zap.Any("COUNT", access))

	for _, subAccess := range access {
		grants, err := createGrantSubuserFromTeammate(resource, &subAccess)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, grants...)
	}

	// Scope grants — only on the first (and only) page to avoid duplicate API calls.
	if opts.PageToken.Token == "" {
		specificTeammate, err := u.client.GetSpecificTeammate(ctx, username)
		if err != nil {
			return nil, nil, err
		}

		for _, scope := range specificTeammate.Scopes {
			if _, ok := SendGridScopes[Scope(scope)]; !ok {
				logger.Debug("baton-sendgrid: skipping unknown scope", zap.String("scope", scope))
				continue
			}
			scopeRs, err := scopeResource(Scope(scope))
			if err != nil {
				return nil, nil, err
			}
			rv = append(rv, grant.NewGrant(scopeRs, assignedEntitlement, resource.Id))
		}
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextToken}, nil
}

func (u *teammateBuilder) Delete(ctx context.Context, resourceId *v2.ResourceId) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if resourceId.ResourceType != teammateResourceType.Id {
		return nil, fmt.Errorf("invalid resource type: expected %s, got %s", teammateResourceType.Id, resourceId.ResourceType)
	}

	username := resourceId.GetResource()
	if username == "" {
		return nil, fmt.Errorf("missing resource ID (username)")
	}

	if err := u.client.DeleteTeammate(ctx, username); err != nil {
		if strings.Contains(err.Error(), "teammate does not exist") {
			l.Warn("Teammate not found, may have been already deleted")
			return nil, nil
		}
		l.Error("failed to delete teammate", zap.Error(err))
		return nil, err
	}

	return nil, nil
}

func newTeammateBuilder(client SendGridClient) *teammateBuilder {
	return &teammateBuilder{
		client: client,
	}
}

func createGrantSubuserFromTeammate(resource *v2.Resource, subAcess *models.TeammateSubuser) ([]*v2.Grant, error) {
	userId, err := rs.NewResourceID(subuserResourceType, subAcess.Id)
	if err != nil {
		return nil, err
	}

	rv := []*v2.Grant{
		grant.NewGrant(resource, accessEntitlement, userId),
	}

	return rv, nil
}
