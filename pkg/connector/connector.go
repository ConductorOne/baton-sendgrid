package connector

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/conductorone/baton-sendgrid/pkg/config"
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

	GetSpecificTeammate(ctx context.Context, username client.Username, onBehalfOf client.OnBehalfOf) (*models.TeammateScope, error)
	// GetSpecificTeammateNoCache bypasses the http cache; required for reads
	// that feed a write, since SetTeammateScopes replaces the whole scope list.
	GetSpecificTeammateNoCache(ctx context.Context, username client.Username, onBehalfOf client.OnBehalfOf) (*models.TeammateScope, error)
	GetTeammates(ctx context.Context, pToken *pagination.Token, onBehalfOf client.OnBehalfOf) ([]*models.Teammate, string, error)
	DeleteTeammate(ctx context.Context, username client.Username, onBehalfOf client.OnBehalfOf) error
	GetTeammatesSubAccess(ctx context.Context, username client.Username, pToken *pagination.Token, onBehalfOf client.OnBehalfOf) ([]*models.TeammateSubuser, string, error)
	GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]*models.TeammateInvitation, string, error)
	DeletePendingTeammate(ctx context.Context, token string) error
	SetTeammateScopes(ctx context.Context, username client.Username, scopes []string, isAdmin bool, onBehalfOf client.OnBehalfOf) error

	GetSubusers(ctx context.Context, pToken *pagination.Token) ([]models.Subuser, string, error)
	GetSubuserUsernameByID(ctx context.Context, subuserID string) (string, error)
	CreateSubuser(ctx context.Context, subuser models.SubuserCreate) error
	DeleteSubuser(ctx context.Context, username string) error
	SetSubuserDisabled(ctx context.Context, username string, disabled bool) error
}

type Connector struct {
	client         SendGridClient
	ignoreSubusers bool
	// skipScopeResourceType reports whether scope is excluded from the sync
	// filter. Named for the skip condition so the zero value is safe: main.go
	// registers a zero-value Connector{} as the capabilities factory.
	skipScopeResourceType bool
}

// ResourceSyncers returns a ResourceSyncerV2 for each resource type that should be synced from the upstream service.
func (d *Connector) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncerV2 {
	return []connectorbuilder.ResourceSyncerV2{
		newTeammateBuilder(d.client, d.skipScopeResourceType),
		newTeammateInvitationBuilder(d.client),
		newScopeBuilder(d.client),
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
				emailKey: {
					DisplayName: "Email",
					Required:    true,
					Description: "Email address of the teammate to invite.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "teammate@example.com",
					Order:       1,
				},
				isAdminKey: {
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
func New(ctx context.Context, sgClient SendGridClient, ignoreSubusers bool, skipScopeResourceType bool) (*Connector, error) {
	if sgClient == nil {
		return nil, ErrSendgridClientNotProvided
	}

	return &Connector{
		client:                sgClient,
		ignoreSubusers:        ignoreSubusers,
		skipScopeResourceType: skipScopeResourceType,
	}, nil
}

// NewLambdaConnector creates a new connector from config for lambda/containerized deployment.
func NewLambdaConnector(ctx context.Context, cfg *config.Sendgrid, opts *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	l := ctxzap.Extract(ctx)

	sendGridApiKey := cfg.SendgridApiKey
	sendgridRegion := cfg.SendgridRegion
	sendgridIgnoreSubusers := cfg.IgnoreSubusers
	baseUrlOverride := cfg.BaseUrl

	var baseUrl string

	if baseUrlOverride != "" {
		baseUrl = baseUrlOverride
	} else {
		switch sendgridRegion {
		case "eu":
			baseUrl = client.SendGridEUBaseUrl
		case "global":
			baseUrl = client.SendGridBaseUrl
		default:
			baseUrl = client.SendGridBaseUrl
			l.Warn("invalid sendgrid region, using the default global URL", zap.String("region", sendgridRegion))
		}
	}

	sendGridClient, err := client.NewClient(ctx, baseUrl, sendGridApiKey)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: error creating client: %w", err)
	}

	// nil opts means no filter, so nothing is skipped.
	skipScopeResourceType := opts != nil && !opts.WillSyncResourceType(scopeResourceType.Id)

	cb, err := New(ctx, sendGridClient, sendgridIgnoreSubusers, skipScopeResourceType)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: error creating connector: %w", err)
	}

	return cb, nil, nil
}
