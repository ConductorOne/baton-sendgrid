package connector

import (
	"context"
	"fmt"
	"strings"

	sgclient "github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	accessEntitlement = "access"

	teammateAtParentScopeSessionKeyPrefix = "teammate_at_parent_scope:"
	subuserUsernameSessionKeyPrefix       = "teammate_subuser_username:"
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
	for _, tm := range teammates {
		res, err := teammateResource(tm, nil, "")
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

	subuserUsername, err := u.getSubuserUsername(ctx, opts, subuserID)
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			l.Debug("baton-sendgrid: missing permission to resolve subuser, skipping its teammates", zap.String("subuser_id", subuserID), zap.Error(err))
			return nil, &rs.SyncOpResults{NextPageToken: ""}, nil
		}
		return nil, nil, err
	}

	teammates, pNextToken, err := u.client.GetTeammates(ctx, &opts.PageToken, sgclient.OnBehalfOf(subuserUsername))
	if err != nil {
		if status.Code(err) == codes.PermissionDenied {
			l.Debug("baton-sendgrid: missing permission to list teammates for subuser, skipping", zap.String("subuser_username", subuserUsername), zap.Error(err))
			return nil, &rs.SyncOpResults{NextPageToken: ""}, nil
		}
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to list teammates for subuser %s: %w", subuserUsername, err)
	}

	var rv []*v2.Resource
	for _, tm := range teammates {
		existsAtParent, err := u.isParentScopeTeammate(ctx, opts, tm.Username)
		if err != nil {
			return nil, nil, err
		}
		if existsAtParent {
			// Already synced at parent scope. Skipping.
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

// getSubuserUsername resolves a subuser's username from its numeric ID.
func (u *teammateBuilder) getSubuserUsername(ctx context.Context, opts rs.SyncOpAttrs, subuserID string) (string, error) {
	key := subuserUsernameSessionKeyPrefix + subuserID
	if opts.Session != nil {
		if raw, found, err := opts.Session.Get(ctx, key); err == nil && found {
			return string(raw), nil
		}
	}

	username, err := u.client.GetSubuserUsernameByID(ctx, subuserID)
	if err != nil {
		return "", err
	}

	if opts.Session != nil {
		_ = opts.Session.Set(ctx, key, []byte(username))
	}

	return username, nil
}

// isParentScopeTeammate reports whether username already exists as a
// parent-scope (global) teammate, used to dedupe sub-account-local teammates
// discovered via on-behalf-of.
func (u *teammateBuilder) isParentScopeTeammate(ctx context.Context, opts rs.SyncOpAttrs, username string) (bool, error) {
	key := teammateAtParentScopeSessionKeyPrefix + username
	if opts.Session != nil {
		if raw, found, err := opts.Session.Get(ctx, key); err == nil && found {
			return string(raw) == "1", nil
		}
	}

	tm, err := u.client.GetSpecificTeammate(ctx, sgclient.Username(username), "")
	if err != nil && status.Code(err) != codes.NotFound {
		return false, fmt.Errorf("baton-sendgrid: failed to check parent-scope teammate %s: %w", username, err)
	}

	exists := tm != nil && tm.Username == username
	if opts.Session != nil {
		val := "0"
		if exists {
			val = "1"
		}
		_ = opts.Session.Set(ctx, key, []byte(val))
	}

	return exists, nil
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
	access, nextToken, err := u.client.GetTeammatesSubAccess(ctx, sgclient.Username(username), &opts.PageToken, sgclient.OnBehalfOf(onBehalfOf))
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to get teammate subuser access for %s: %w", username, err)
	}

	logger := ctxzap.Extract(ctx)
	logger.Info("Teammate grants", zap.String("username", username), zap.Any("COUNT", access))

	for _, subAccess := range access {
		grants, err := createGrantSubuserFromTeammate(resource, subAccess)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, grants...)
	}

	// Scope grants — only on the first (and only) page to avoid duplicate API calls.
	if opts.PageToken.Token == "" {
		specificTeammate, err := u.client.GetSpecificTeammate(ctx, sgclient.Username(username), sgclient.OnBehalfOf(onBehalfOf))
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

	if err := u.client.DeleteTeammate(ctx, sgclient.Username(username), sgclient.OnBehalfOf(onBehalfOf)); err != nil {
		if status.Code(err) == codes.NotFound || strings.Contains(err.Error(), "teammate does not exist") {
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
