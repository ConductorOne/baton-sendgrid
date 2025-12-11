package connector

import (
	"context"
	"fmt"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type teammateInvitationBuilder struct {
	client SendGridClient
}

func (u *teammateInvitationBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return teammateInvitationResourceType
}

func (u *teammateInvitationBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	logger := ctxzap.Extract(ctx)

	invitations, pNextToken, err := u.client.GetPendingTeammates(ctx, pToken)
	if err != nil {
		return nil, "", nil, err
	}

	rv := make([]*v2.Resource, 0, len(invitations))
	for i, invitation := range invitations {
		res, err := teammateInvitationResource(invitation)
		if err != nil {
			logger.Error("Failed to create teammate invitation resource",
				zap.Error(err),
				zap.Int("index", i),
				zap.String("email", invitation.Email))
			continue
		}

		rv = append(rv, res)
	}

	nextToken := ""
	if len(invitations) != 0 {
		nextToken = pNextToken
	}

	return rv, nextToken, nil, nil
}

func (u *teammateInvitationBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *teammateInvitationBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	return nil, "", nil, nil
}

func (u *teammateInvitationBuilder) CreateAccountCapabilityDetails(_ context.Context) (*v2.CredentialDetailsAccountProvisioning, annotations.Annotations, error) {
	return &v2.CredentialDetailsAccountProvisioning{
		SupportedCredentialOptions: []v2.CapabilityDetailCredentialOption{
			v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
		},
		PreferredCredentialOption: v2.CapabilityDetailCredentialOption_CAPABILITY_DETAIL_CREDENTIAL_OPTION_NO_PASSWORD,
	}, nil, nil
}

// CreateAccount invites a new teammate by email with scopes and admin flag.
func (u *teammateInvitationBuilder) CreateAccount(
	ctx context.Context,
	accountInfo *v2.AccountInfo,
	credentialOptions *v2.LocalCredentialOptions,
) (
	connectorbuilder.CreateAccountResponse,
	[]*v2.PlaintextData,
	annotations.Annotations,
	error,
) {
	if accountInfo == nil {
		return nil, nil, nil, fmt.Errorf("accountInfo cannot be nil")
	}

	// Extract primary email (preferred for SendGrid teammate invitation).
	email := ""
	for _, e := range accountInfo.GetEmails() {
		if e.GetIsPrimary() {
			email = e.GetAddress()
			break
		}
	}
	if email == "" {
		if len(accountInfo.GetEmails()) > 0 {
			email = accountInfo.GetEmails()[0].GetAddress()
		}
	}
	if email == "" {
		return nil, nil, nil, fmt.Errorf("email is required to create a teammate invitation")
	}

	// Optional inputs from profile: scopes ([]string) and is_admin (bool).
	var scopes []string
	isAdmin := false
	if accountInfo.GetProfile() != nil {
		if v, ok := accountInfo.GetProfile().AsMap()["scopes"]; ok {
			if list, ok := v.([]interface{}); ok {
				for _, it := range list {
					if s, ok := it.(string); ok && s != "" {
						scopes = append(scopes, s)
					}
				}
			}
		}
		if v, ok := accountInfo.GetProfile().AsMap()["is_admin"]; ok {
			if b, ok := v.(bool); ok {
				isAdmin = b
			}
		}
	}

	// Invite the teammate.
	invitation, err := u.client.InviteTeammate(ctx, email, scopes, isAdmin)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to invite teammate: %w", err)
	}

	// Build a pending invitation resource
	res, err := teammateInvitationResource(invitation)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to build teammate invitation resource: %w", err)
	}

	car := &v2.CreateAccountResponse_SuccessResult{
		Resource: res,
	}

	return car, nil, nil, nil
}

func (u *teammateInvitationBuilder) Delete(ctx context.Context, resourceId *v2.ResourceId) (annotations.Annotations, error) {
	l := ctxzap.Extract(ctx)

	if resourceId.ResourceType != teammateInvitationResourceType.Id {
		return nil, fmt.Errorf("invalid resource type: expected %s, got %s", teammateInvitationResourceType.Id, resourceId.ResourceType)
	}

	token := resourceId.GetResource()
	if token == "" {
		return nil, fmt.Errorf("missing resource ID (token)")
	}

	if err := u.client.DeletePendingTeammate(ctx, token); err != nil {
		// Check if it's a "not found" error from SendGrid
		if strings.Contains(err.Error(), "unable to find pending invite") {
			l.Warn("Pending teammate invitation not found, may have been already deleted")
			return nil, nil
		}
		l.Error("failed to delete pending teammate invitation", zap.Error(err))
		return nil, err
	}

	return nil, nil
}

func newTeammateInvitationBuilder(client SendGridClient) *teammateInvitationBuilder {
	return &teammateInvitationBuilder{
		client: client,
	}
}
