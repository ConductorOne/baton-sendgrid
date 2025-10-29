package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
)

// The user resource type is for all user objects from the database.
var (
	teammateResourceType = &v2.ResourceType{
		Id:          "teammate",
		DisplayName: "teammate",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}

	teammateInvitationResourceType = &v2.ResourceType{
		Id:          "teammate_invitation",
		DisplayName: "Teammate Invitation",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
		Annotations: annotations.New(&v2.SkipEntitlementsAndGrants{}),
	}

	scopeResourceType = &v2.ResourceType{
		Id:          "scope",
		DisplayName: "Scope",
	}

	subuserResourceType = &v2.ResourceType{
		Id:          "subuser",
		DisplayName: "Subuser",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}
)
