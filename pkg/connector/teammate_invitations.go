package connector

import (
	"context"
	"fmt"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

type teammateInvitationBuilder struct {
	client SendGridClient
}

func (u *teammateInvitationBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return teammateInvitationResourceType
}

func (u *teammateInvitationBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	logger := ctxzap.Extract(ctx)

	invitations, pNextToken, err := u.client.GetPendingTeammates(ctx, &opts.PageToken)
	if err != nil {
		return nil, nil, err
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

	return rv, &rs.SyncOpResults{NextPageToken: nextToken}, nil
}

func (u *teammateInvitationBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, nil, nil
}

func (u *teammateInvitationBuilder) Grants(ctx context.Context, resource *v2.Resource, opts rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, nil, nil
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

	// Optional inputs from profile: is_admin and scopes.
	// Note: SendGrid API does not allow specifying scopes when teammate is an admin.
	// Admin teammates automatically have full access to all permissions.
	var scopes []string
	isAdmin := DefaultTeammateIsAdmin

	if accountInfo.GetProfile() != nil {
		profileMap := accountInfo.GetProfile().AsMap()

		// Parse is_admin (BoolField).
		if v, ok := profileMap["is_admin"]; ok {
			adminVal, ok := v.(bool)
			if !ok {
				return nil, nil, nil, fmt.Errorf("is_admin must be a boolean, got %T", v)
			}
			isAdmin = adminVal
		}

		// Parse scopes only if not an admin (SendGrid constraint: cannot specify scopes for admins).
		if !isAdmin {
			if v, ok := profileMap["scopes"]; ok {
				scopeList, ok := v.([]interface{})
				if !ok {
					return nil, nil, nil, fmt.Errorf("scopes must be a list, got %T", v)
				}
				for _, item := range scopeList {
					s, ok := item.(string)
					if !ok {
						return nil, nil, nil, fmt.Errorf("scope item must be a string, got %T", item)
					}
					if s != "" {
						scopes = append(scopes, s)
					}
				}
			}
		}
	}

	// Apply default scope for non-admin teammates if no scopes were provided.
	// This matches the DefaultValue in the AccountCreationSchema.
	if !isAdmin && len(scopes) == 0 {
		scopes = DefaultTeammateScopes
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
