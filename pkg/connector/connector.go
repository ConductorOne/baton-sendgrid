package connector

import (
	"context"
	"errors"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"io"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
)

var (
	ErrSendgridClientNotProvided = errors.New("sendgrid client not provided")
)

type Connector struct {
	client     *client.SendGridClient
	scopeCache *scopeCache
}

// ResourceSyncers returns a ResourceSyncer for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		newUserBuilder(d.client),
		newScopeBuilder(d.client, d.scopeCache),
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
func New(ctx context.Context, client *client.SendGridClient) (*Connector, error) {
	if client == nil {
		return nil, ErrSendgridClientNotProvided
	}

	return &Connector{
		client:     client,
		scopeCache: newScopeCache(client),
	}, nil
}
