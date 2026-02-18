package connector

import (
	"context"
	"errors"
	"io"

	cfg "github.com/conductorone/baton-sendgrid/pkg/config"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

var (
	ErrSendgridClientNotProvided = errors.New("sendgrid client not provided")
)

// DefaultTeammateIsAdmin is the default admin status for new teammates.
// By default, teammates are not admins and require explicit scopes.
var DefaultTeammateIsAdmin = false

// DefaultTeammateScopes is the default scope assigned to non-admin teammates when no scopes are specified.
// user.profile.read is the most restrictive read-only scope, allowing only viewing of profile information.
var DefaultTeammateScopes = []string{"user.profile.read"}

type SendGridClient interface {
	InviteTeammate(ctx context.Context, email string, scopes []string, isAdmin bool) (*models.TeammateInvitation, error)

	GetSpecificTeammate(ctx context.Context, username string) (*models.TeammateScope, error)
	GetTeammates(ctx context.Context, pToken *pagination.Token) ([]models.Teammate, string, error)
	DeleteTeammate(ctx context.Context, username string) error
	GetTeammatesSubAccess(ctx context.Context, username string, pToken *pagination.Token) ([]models.TeammateSubuser, string, error)
	GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]*models.TeammateInvitation, string, error)
	DeletePendingTeammate(ctx context.Context, token string) error
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
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncerV2 {
	return []connectorbuilder.ResourceSyncerV2{
		newTeammateBuilder(d.client),
		newTeammateInvitationBuilder(d.client),
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
		AccountCreationSchema: &v2.ConnectorAccountCreationSchema{
			FieldMap: map[string]*v2.ConnectorAccountCreationSchema_Field{
				"email": {
					DisplayName: "Email",
					Required:    true,
					Description: "Email address of the teammate to invite.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "teammate@example.com",
					Order:       1,
				},
				"is_admin": {
					DisplayName: "Is Admin",
					Required:    false,
					Description: "Whether the teammate should have admin privileges. Admin teammates have full access to all permissions.",
					Field: &v2.ConnectorAccountCreationSchema_Field_BoolField{
						BoolField: &v2.ConnectorAccountCreationSchema_BoolField{DefaultValue: &DefaultTeammateIsAdmin},
					},
					Order: 2,
				},
				"scopes": {
					DisplayName: "Scopes",
					Required:    false,
					Description: "List of scopes to assign to the teammate. Required for non-admin teammates. Ignored when teammate is admin.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringListField{
						StringListField: &v2.ConnectorAccountCreationSchema_StringListField{
							DefaultValue: DefaultTeammateScopes,
						},
					},
					Placeholder: "mail.send",
					Order:       3,
				},
			},
		},
	}, nil
}

// Validate is called to ensure that the connector is properly configured. It should exercise any API credentials
// to be sure that they are valid.
func (d *Connector) Validate(ctx context.Context) (annotations.Annotations, error) {
	return nil, nil
}

// New returns a new instance of the connector.
func New(ctx context.Context, cc *cfg.Sendgrid, opts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	l := ctxzap.Extract(ctx)

	sendGridApiKey := cc.SendgridApiKey
	sendgridRegion := cc.SendgridRegion
	sendgridIgnoreSubusers := cc.IgnoreSubusers

	var baseUrl string

	switch sendgridRegion {
	case "eu":
		baseUrl = client.SendGridEUBaseUrl
	case "global":
		baseUrl = client.SendGridBaseUrl
	default:
		baseUrl = client.SendGridBaseUrl
		l.Warn("invalid sendgrid region, using the default global URL", zap.String("region", sendgridRegion))
	}

	sendGridClient, err := client.NewClient(ctx, baseUrl, sendGridApiKey)
	if err != nil {
		l.Error("error creating sendgrid client", zap.Error(err))
		return nil, nil, err
	}

	return &Connector{
		client:         sendGridClient,
		scopeCache:     newScopeCache(sendGridClient),
		ignoreSubusers: sendgridIgnoreSubusers,
	}, nil, nil
}
