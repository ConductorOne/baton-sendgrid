package connector

import (
	"fmt"
	"strings"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

func teammateResource(user *models.Teammate) (*v2.Resource, error) {
	var userStatus = v2.UserTrait_Status_STATUS_ENABLED

	profile := map[string]interface{}{
		"username":       user.Username,
		"user_type":      user.UserType,
		"email":          user.Email,
		"is_sso":         user.IsSso,
		"is_admin":       user.IsAdmin,
		"is_unified":     user.IsUnified,
		"is_partner_sso": user.IsPartnerSso,
	}

	userTraits := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithStatus(userStatus),
		rs.WithEmail(user.Email, true),
		rs.WithUserLogin(user.Email),
	}

	ret, err := rs.NewUserResource(
		user.Username,
		teammateResourceType,
		// Twilio doesn't have a unique ID for users, so we use the username as the ID
		user.Username,
		userTraits,
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

	roleTraitOptions := []rs.RoleTraitOption{
		rs.WithRoleProfile(profile),
	}

	resource, err := rs.NewRoleResource(
		string(scope),
		scopeResourceType,
		string(scope),
		roleTraitOptions,
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}

func subuserResource(subuser models.Subuser) (*v2.Resource, error) {
	status := v2.UserTrait_Status_STATUS_ENABLED

	if subuser.Disabled {
		status = v2.UserTrait_Status_STATUS_DISABLED
	}

	profile := map[string]interface{}{
		"id":       subuser.ID,
		"username": subuser.Username,
		"email":    subuser.Email,
		"disabled": subuser.Disabled,
	}

	subUserTraitOptions := rs.WithUserTrait(
		rs.WithUserProfile(profile),
		rs.WithStatus(status),
		rs.WithEmail(subuser.Email, true),
	)

	resource, err := rs.NewResource(
		subuser.Username,
		subuserResourceType,
		subuser.ID,
		subUserTraitOptions,
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
		"is_admin":        invitation.IsAdmin,
		"expiration_date": invitation.ExpirationDate,
	}

	userTraits := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithStatus(v2.UserTrait_Status_STATUS_DISABLED), // Pending invitations are always in a "Disabled" status
		rs.WithEmail(invitation.Email, true),
		rs.WithUserLogin(invitation.Email),
	}

	ret, err := rs.NewUserResource(
		"(Invitation) "+invitation.Email,
		teammateInvitationResourceType,
		resourceID,
		userTraits,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create teammate invitation resource: %w", err)
	}

	return ret, nil
}
