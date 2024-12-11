package connector

import (
	"context"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
)

func userResource(ctx context.Context, user *client.SubUserAccess, parentResourceID *v2.ResourceId) (*v2.Resource, error) {
	var userStatus = v2.UserTrait_Status_STATUS_ENABLED

	if user.Disabled {
		userStatus = v2.UserTrait_Status_STATUS_DISABLED
	}

	// TODO: needs to understand which information should I add to the profile
	profile := map[string]interface{}{
		"user_id":   user.Id,
		"user_name": user.Username,
	}

	// TODO: needs to understand which information should I add to the trait
	userTraits := []rs.UserTraitOption{
		rs.WithUserProfile(profile),
		rs.WithStatus(userStatus),
	}

	ret, err := rs.NewUserResource(
		user.Username,
		userResourceType,
		user.Id,
		userTraits,
		rs.WithParentResourceID(parentResourceID))
	if err != nil {
		return nil, err
	}

	return ret, nil
}
