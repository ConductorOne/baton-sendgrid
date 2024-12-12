package connector

import (
	"context"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
)

func userResource(ctx context.Context, user *client.Teammate, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
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
	}

	ret, err := rs.NewUserResource(
		user.Username,
		userResourceType,
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
