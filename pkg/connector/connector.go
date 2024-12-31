package connector

import (
	"context"
	"errors"
	"io"

	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
)

var (
	ErrSendgridClientNotProvided = errors.New("sendgrid client not provided")
)

type SendGridClient interface {
	InviteTeammate(ctx context.Context, email string, scopes []string, isAdmin bool) error
	DeleteTeammate(ctx context.Context, username string) error

	GetSpecificTeammate(ctx context.Context, username string) (*models.TeammateScope, error)
	GetTeammates(ctx context.Context, pToken *pagination.Token) ([]models.Teammate, string, error)
	GetTeammatesSubAccess(ctx context.Context, username string, pToken *pagination.Token) ([]models.TeammateSubuser, string, error)
	GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]models.PendingUserAccess, string, error)
	SetTeammateScopes(ctx context.Context, username string, scopes []string, isAdmin bool) error

	GetSubusers(ctx context.Context, pToken *pagination.Token) ([]models.Subuser, string, error)
	CreateSubuser(ctx context.Context, subuser models.SubuserCreate) error
	DeleteSubuser(ctx context.Context, username string) error
	SetSubuserDisabled(ctx context.Context, username string, disabled bool) error
}

type Connector struct {
	client         SendGridClient
	scopeCache     *scopeCache
	ignoreSubusers bool
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		newTeammateBuilder(d.client),
		newScopeBuilder(d.client, d.scopeCache),
		newSubuserBuilder(d.client, d.ignoreSubusers),
	}
}

// Asset takes an input AssetRef and attempts to fetch it using the connector's.json authenticated http client
// It streams a response, always starting with a metadata object, following by chunked payloads for the asset.
func (d *Connector) Asset(ctx context.Context, asset *v2.AssetRef) (string, io.ReadCloser, error) {
	return "", nil, nil
}

// Metadata returns metadata about the connector.
func (d *Connector) Metadata(ctx context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Sendgrid",
		Description: "Connector syncing Sendgrid teammates to Baton.",
	}, nil
}

// Validate is called to ensure that the connector is properly configured. It should exercise any API credentials
// to be sure that they are valid.
func (d *Connector) Validate(ctx context.Context) (annotations.Annotations, error) {
	return nil, nil
}

// New returns a new instance of the connector.
func New(ctx context.Context, client SendGridClient, ignoreSubusers bool) (*Connector, error) {
	if client == nil {
		return nil, ErrSendgridClientNotProvided
	}

	return &Connector{
		client:         client,
		scopeCache:     newScopeCache(client),
		ignoreSubusers: ignoreSubusers,
	}, nil
}
