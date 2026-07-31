package connector

import (
	"context"
	"fmt"
	"strings"

	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
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

// List has two flows, driven by whether the SDK calls it with a parent
// resource (subuserResource sets the ChildResourceType annotation, so the
// SDK calls List() once per synced subuser with that subuser as parent, in
// addition to the normal unparented call for the resource type):
//
//   - No parent: the parent-scope (global) teammates, unchanged. Every
//     teammate returned here has no ParentResourceId.
//   - Parent is a subuser: lists that subuser's teammates via the
//     on-behalf-of header (a mix of global admins and sub-account-local
//     teammates), and only emits the ones that don't also exist at parent
//     scope — otherwise a global admin would be re-emitted under every
//     subuser they can reach, flipping their ParentResourceId depending on
//     scan order.
func (u *teammateBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	if parentResourceID == nil {
		return u.listRootTeammates(ctx, opts)
	}

	return u.listSubuserTeammates(ctx, parentResourceID, opts)
}

func (u *teammateBuilder) listRootTeammates(ctx context.Context, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	teammates, pNextToken, err := u.client.GetTeammates(ctx, &opts.PageToken, "")
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to list teammates: %w", err)
	}

	rv := make([]*v2.Resource, 0, len(teammates))
	for i := range teammates {
		res, err := teammateResource(&teammates[i], nil, "")
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, res)
	}

	nextToken := ""
	if len(teammates) != 0 {
		nextToken = pNextToken
	}

	return rv, &rs.SyncOpResults{NextPageToken: nextToken}, nil
}

func (u *teammateBuilder) listSubuserTeammates(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	l := ctxzap.Extract(ctx)
	subuserID := parentResourceID.GetResource()

	subuserUsername, err := u.client.GetSubuserUsernameByID(ctx, subuserID)
	if err != nil {
		if client.IsForbiddenErr(err) {
			l.Debug("baton-sendgrid: missing permission to resolve subuser, skipping its teammates", zap.String("subuser_id", subuserID), zap.Error(err))
			return nil, &rs.SyncOpResults{NextPageToken: ""}, nil
		}
		return nil, nil, err
	}

	teammates, pNextToken, err := u.client.GetTeammates(ctx, &opts.PageToken, subuserUsername)
	if err != nil {
		if client.IsForbiddenErr(err) {
			l.Debug("baton-sendgrid: missing permission to list teammates for subuser, skipping", zap.String("subuser_username", subuserUsername), zap.Error(err))
			return nil, &rs.SyncOpResults{NextPageToken: ""}, nil
		}
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to list teammates for subuser %s: %w", subuserUsername, err)
	}

	rv := make([]*v2.Resource, 0, len(teammates))
	for i := range teammates {
		tm := &teammates[i]

		existsAtParent, err := u.client.TeammateExistsAtParentScope(ctx, tm.Username)
		if err != nil {
			return nil, nil, err
		}
		if existsAtParent {
			// Already synced at parent scope. Re-emitting it here would flip
			// its ParentResourceId depending on which subuser happens to be
			// scanned.
			continue
		}

		res, err := teammateResource(tm, parentResourceID, subuserUsername)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, res)
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

	onBehalfOf, err := teammateOnBehalfOf(ctx, u.client, resource)
	if err != nil {
		return nil, nil, err
	}

	// Subuser access grants.
	access, nextToken, err := u.client.GetTeammatesSubAccess(ctx, username, &opts.PageToken, onBehalfOf)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to get teammate subuser access for %s: %w", username, err)
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
		specificTeammate, err := u.client.GetSpecificTeammate(ctx, username, onBehalfOf)
		if err != nil {
			return nil, nil, fmt.Errorf("baton-sendgrid: failed to get teammate %s: %w", username, err)
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

func (u *teammateBuilder) Delete(ctx context.Context, resourceId *v2.ResourceId, parentResourceID *v2.ResourceId) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if resourceId.ResourceType != teammateResourceType.Id {
		return nil, fmt.Errorf("baton-sendgrid: invalid resource type: expected %s, got %s", teammateResourceType.Id, resourceId.ResourceType)
	}

	username := resourceId.GetResource()
	if username == "" {
		return nil, fmt.Errorf("baton-sendgrid: missing resource ID (username)")
	}

	onBehalfOf, err := resolveOnBehalfOfByParentID(ctx, u.client, parentResourceID)
	if err != nil {
		return nil, err
	}

	if err := u.client.DeleteTeammate(ctx, username, onBehalfOf); err != nil {
		if client.IsNotFoundErr(err) || strings.Contains(err.Error(), "teammate does not exist") {
			l.Warn("baton-sendgrid: teammate not found, may have been already deleted", zap.String("username", username))
			return nil, nil
		}
		return nil, fmt.Errorf("baton-sendgrid: failed to delete teammate %s: %w", username, err)
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
