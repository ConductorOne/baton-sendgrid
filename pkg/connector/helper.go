package connector

import (
	"context"
	"fmt"
	"strings"

	sgclient "github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	emailKey                  = "email"
	isAdminKey                = "is_admin"
	subuserUsernameProfileKey = "subuser_username"
)

// teammateOnBehalfOf returns the on-behalf-of subuser username for a
// teammate resource, or "" for a parent-scope teammate. It prefers the
// subuser_username stashed in the resource's own profile at sync time
// (resource.GetProfile() — the base-Resource profile field that
// WithUserProfile/WithResourceProfile populate — no extra API call needed).
// It only falls back to resolving the subuser's username from its numeric
// resource ID via the client if the profile doesn't have it (e.g. a
// resource synced before this field existed).
func teammateOnBehalfOf(ctx context.Context, client SendGridClient, resource *v2.Resource) (string, error) {
	parent := resource.GetParentResourceId()
	if parent == nil {
		return "", nil
	}

	if profile := resource.GetProfile(); profile != nil {
		if v, ok := profile.GetFields()[subuserUsernameProfileKey]; ok {
			if username := v.GetStringValue(); username != "" {
				return username, nil
			}
		}
	}

	// Legacy-data fallback: teammateResource always stashes subuser_username
	// on the profile for subuser-scoped teammates synced by the current
	// code, so Grant/Revoke (the only callers that reach here without a
	// fresher sync in between) are not likely to hit this — it only applies
	// to a resource synced before that profile field existed.
	return resolveOnBehalfOfByParentID(ctx, client, parent)
}

// getTeammateWithFreshOnBehalfOf fetches the teammate using onBehalfOf, which
// may be a stale subuser username cached on the resource's profile. If
// SendGrid reports not-found — e.g. the subuser was renamed since the cache
// was written — it re-resolves the current username from the subuser's
// stable numeric ID and retries once. Grant/Revoke/Delete are infrequent,
// one-off calls, so this re-resolution is a plain, uncached client call each
// time — not worth caching. Returns the teammate and the on-behalf-of value
// that worked, so callers can reuse it for follow-up calls (e.g.
// SetTeammateScopes).
//
// The teammate read itself deliberately bypasses the http cache: every caller
// is a provisioning path that turns the returned Scopes into a full-list
// SetTeammateScopes write, and a cached read would make consecutive tasks on
// the same teammate overwrite each other's scopes.
func getTeammateWithFreshOnBehalfOf(ctx context.Context, client SendGridClient, principal *v2.Resource, username, onBehalfOf string) (*models.TeammateScope, string, error) {
	teammate, err := client.GetSpecificTeammateNoCache(ctx, sgclient.Username(username), sgclient.OnBehalfOf(onBehalfOf))
	if err == nil || onBehalfOf == "" || status.Code(err) != codes.NotFound {
		return teammate, onBehalfOf, err
	}

	// Last-resort retry: only reached when the cached onBehalfOf just 404'd,
	// which should be rare (a subuser rename since the cache was written).
	freshOnBehalfOf, resolveErr := resolveOnBehalfOfByParentID(ctx, client, principal.GetParentResourceId())
	if resolveErr != nil {
		return nil, onBehalfOf, err
	}

	teammate, err = client.GetSpecificTeammateNoCache(ctx, sgclient.Username(username), sgclient.OnBehalfOf(freshOnBehalfOf))
	return teammate, freshOnBehalfOf, err
}

// resolveOnBehalfOfByParentID resolves the on-behalf-of subuser username
// from a bare parent ResourceId, for callers that don't have the full
// resource available — namely Delete, which the SDK only passes IDs to, so
// there's no profile to read the username from. SendGrid's teammate
// endpoints only accept a subuser *username* in the on-behalf-of header, but
// a resource's parent only carries the subuser's numeric resource ID, so
// this resolves it via the client. Grant/Revoke/Delete happen occasionally,
// not in a pagination loop, so this is a plain, uncached call — no
// caching needed for this path.
func resolveOnBehalfOfByParentID(ctx context.Context, client SendGridClient, parentResourceID *v2.ResourceId) (string, error) {
	if parentResourceID == nil {
		return "", nil
	}

	username, err := client.GetSubuserUsernameByID(ctx, parentResourceID.GetResource())
	if err != nil {
		return "", fmt.Errorf("baton-sendgrid: failed to resolve on-behalf-of subuser: %w", err)
	}

	return username, nil
}

// teammateResource builds a teammate resource. parentResourceID/subuserUsername
// are empty for a parent-scope (global) teammate, or set to the owning
// subuser's resource ID and username for a teammate that only exists inside
// that sub-account. The resource ID is always the bare username, regardless
// of scope, so a sub-account-local teammate and a global teammate use
// identical ID construction. subuserUsername is stashed on the resource's
// own profile so Grant/Revoke/Delete can recover the on-behalf-of value
// without an extra API call — see teammateOnBehalfOf.
func teammateResource(user *models.Teammate, parentResourceID *v2.ResourceId, subuserUsername string) (*v2.Resource, error) {
	provisionedScope := "global"
	if parentResourceID != nil {
		provisionedScope = "subuser"
	}

	profile := map[string]interface{}{
		"username":          user.Username,
		"user_type":         user.UserType,
		emailKey:            user.Email,
		"is_sso":            user.IsSso,
		isAdminKey:          user.IsAdmin,
		"is_unified":        user.IsUnified,
		"is_partner_sso":    user.IsPartnerSso,
		"provisioned_scope": provisionedScope,
	}
	if subuserUsername != "" {
		profile[subuserUsernameProfileKey] = subuserUsername
	}

	userTraits := []rs.UserTraitOption{
		rs.WithEmail(user.Email, true),
		rs.WithUserLogin(user.Email),
	}

	// Set explicitly via the non-deprecated, resource-level options since
	// teammateOnBehalfOf reads the profile straight off resource.GetProfile().
	opts := []rs.ResourceOption{
		rs.WithResourceProfile(profile),
		rs.WithResourceStatus(v2.Status_RESOURCE_STATUS_ENABLED, ""),
	}
	if parentResourceID != nil {
		opts = append(opts, rs.WithParentResourceID(parentResourceID))
	}

	ret, err := rs.NewUserResource(
		user.Username,
		teammateResourceType,
		// Twilio doesn't have a unique ID for users, so we use the username as the ID
		user.Username,
		userTraits,
		opts...,
	)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func scopeResource(scope Scope) (*v2.Resource, error) {
	profile := map[string]interface{}{
		"name": string(scope),
	}

	resource, err := rs.NewRoleResource(
		string(scope),
		scopeResourceType,
		string(scope),
		nil,
		rs.WithResourceProfile(profile),
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}

func subuserResource(subuser models.Subuser) (*v2.Resource, error) {
	status := v2.Status_RESOURCE_STATUS_ENABLED

	if subuser.Disabled {
		status = v2.Status_RESOURCE_STATUS_DISABLED
	}

	profile := map[string]interface{}{
		"id":       subuser.Id,
		"username": subuser.Username,
		emailKey:   subuser.Email,
		"disabled": subuser.Disabled,
	}

	resource, err := rs.NewResource(
		subuser.Username,
		subuserResourceType,
		subuser.Id,
		rs.WithUserTrait(rs.WithEmail(subuser.Email, true)),
		rs.WithResourceProfile(profile),
		rs.WithResourceStatus(status, ""),
		// A subuser can have teammates provisioned directly inside it.
		rs.WithAnnotation(&v2.ChildResourceType{ResourceTypeId: teammateResourceType.Id}),
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}

func teammateInvitationResource(invitation *models.TeammateInvitation) (*v2.Resource, error) {
	resourceID := strings.TrimSpace(invitation.Token)

	// Validate that the token is not empty after trimming
	if resourceID == "" {
		return nil, fmt.Errorf("teammateInvitationResource: empty invitation token")
	}

	// Convert []string to []interface{} for protobuf compatibility
	scopes := make([]interface{}, len(invitation.Scopes))
	for i, scope := range invitation.Scopes {
		scopes[i] = scope
	}

	profile := map[string]interface{}{
		"token":           invitation.Token,
		"scopes":          scopes,
		isAdminKey:        invitation.IsAdmin,
		"expiration_date": invitation.ExpirationDate,
	}

	userTraits := []rs.UserTraitOption{
		rs.WithEmail(invitation.Email, true),
		rs.WithUserLogin(invitation.Email),
	}

	ret, err := rs.NewUserResource(
		"(Invitation) "+invitation.Email,
		teammateInvitationResourceType,
		resourceID,
		userTraits,
		rs.WithResourceProfile(profile),
		// Pending invitations are always in a "Disabled" status.
		rs.WithResourceStatus(v2.Status_RESOURCE_STATUS_DISABLED, ""),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create teammate invitation resource: %w", err)
	}

	return ret, nil
}
