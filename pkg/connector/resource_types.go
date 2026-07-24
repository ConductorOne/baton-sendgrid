package connector

import (
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
)

// Resource type IDs, exported so callers (e.g. connector.go) can check
// sync-filter membership via cli.ConnectorOpts.WillSyncResourceType without
// duplicating the string literals.
const (
	ScopeResourceTypeID   = "scope"
	SubuserResourceTypeID = "subuser"
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
		Annotations: annotations.New(
			&v2.SkipEntitlementsAndGrants{},
			&v2.SkipSyncAnomalyDetection{},
		),
	}

	scopeResourceType = &v2.ResourceType{
		Id:          ScopeResourceTypeID,
		DisplayName: "Scope",
	}

	subuserResourceType = &v2.ResourceType{
		Id:          SubuserResourceTypeID,
		DisplayName: "Subuser",
		Traits:      []v2.ResourceType_Trait{v2.ResourceType_TRAIT_USER},
	}
)
