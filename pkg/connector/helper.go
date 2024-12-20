package connector

import (
	"context"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
)

func teammateResource(ctx context.Context, user *models.Teammate, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
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

func scopeResource(ctx context.Context, scope Scope, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
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

func subuserResource(ctx context.Context, subuser models.Subuser, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	status := v2.UserTrait_Status_STATUS_ENABLED

	if subuser.Disabled {
		status = v2.UserTrait_Status_STATUS_DISABLED
	}

	profile := map[string]interface{}{
		"id":       subuser.Id,
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
		subuser.Id,
		subUserTraitOptions,
	)

	if err != nil {
		return nil, err
	}

	return resource, nil
}
