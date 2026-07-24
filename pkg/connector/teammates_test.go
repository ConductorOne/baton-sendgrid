package connector

import (
	"context"
	"testing"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/require"
)

// fakeSendGridClient implements SendGridClient. Only GetSpecificTeammate and
// GetTeammatesSubAccess have real fixture behavior for these tests; every
// other method panics if called since Grants() should never reach them.
type fakeSendGridClient struct {
	getSpecificTeammateCalls   int
	getTeammatesSubAccessCalls int
}

func (f *fakeSendGridClient) InviteTeammate(ctx context.Context, email string, scopes []string, isAdmin bool) (*models.TeammateInvitation, error) {
	panic("not implemented")
}

func (f *fakeSendGridClient) GetSpecificTeammate(ctx context.Context, username string) (*models.TeammateScope, error) {
	f.getSpecificTeammateCalls++
	return &models.TeammateScope{
		Teammate: models.Teammate{Username: username},
		Scopes:   []string{"access_settings.activity.read"},
	}, nil
}

func (f *fakeSendGridClient) GetTeammates(ctx context.Context, pToken *pagination.Token) ([]models.Teammate, string, error) {
	panic("not implemented")
}

func (f *fakeSendGridClient) DeleteTeammate(ctx context.Context, username string) error {
	panic("not implemented")
}

func (f *fakeSendGridClient) GetTeammatesSubAccess(ctx context.Context, username string, pToken *pagination.Token) ([]models.TeammateSubuser, string, error) {
	f.getTeammatesSubAccessCalls++
	return []models.TeammateSubuser{
		{Id: 1, Username: "sub1", Email: "sub1@example.com"},
	}, "", nil
}

func (f *fakeSendGridClient) GetPendingTeammates(ctx context.Context, pToken *pagination.Token) ([]*models.TeammateInvitation, string, error) {
	panic("not implemented")
}

func (f *fakeSendGridClient) DeletePendingTeammate(ctx context.Context, token string) error {
	panic("not implemented")
}

func (f *fakeSendGridClient) SetTeammateScopes(ctx context.Context, username string, scopes []string, isAdmin bool) error {
	panic("not implemented")
}

func (f *fakeSendGridClient) GetSubusers(ctx context.Context, pToken *pagination.Token) ([]models.Subuser, string, error) {
	panic("not implemented")
}

func (f *fakeSendGridClient) CreateSubuser(ctx context.Context, subuser models.SubuserCreate) error {
	panic("not implemented")
}

func (f *fakeSendGridClient) DeleteSubuser(ctx context.Context, username string) error {
	panic("not implemented")
}

func (f *fakeSendGridClient) SetSubuserDisabled(ctx context.Context, username string, disabled bool) error {
	panic("not implemented")
}

func testTeammateResource(t *testing.T) *v2.Resource {
	t.Helper()
	res, err := teammateResource(&models.Teammate{Username: "teammate1", Email: "teammate1@example.com"})
	require.NoError(t, err)
	return res
}

func isScopeGrant(g *v2.Grant) bool {
	return g.GetEntitlement().GetResource().GetId().GetResourceType() == ScopeResourceTypeID
}

func isSubuserGrant(g *v2.Grant) bool {
	return g.GetPrincipal().GetId().GetResourceType() == SubuserResourceTypeID
}

func TestTeammateBuilderGrants_BothEnabled(t *testing.T) {
	client := &fakeSendGridClient{}
	builder := newTeammateBuilder(client, true, true)
	resource := testTeammateResource(t)

	grants, results, err := builder.Grants(context.Background(), resource, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.NotNil(t, results)

	var scopeGrants, subuserGrants int
	for _, g := range grants {
		if isScopeGrant(g) {
			scopeGrants++
		}
		if isSubuserGrant(g) {
			subuserGrants++
		}
	}

	require.Equal(t, 1, scopeGrants, "expected one scope grant")
	require.Equal(t, 1, subuserGrants, "expected one subuser grant")
	require.Len(t, grants, 2)
}

func TestTeammateBuilderGrants_BothDisabled(t *testing.T) {
	client := &fakeSendGridClient{}
	builder := newTeammateBuilder(client, false, false)
	resource := testTeammateResource(t)

	grants, results, err := builder.Grants(context.Background(), resource, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.NotNil(t, results)
	require.Empty(t, grants)
	require.Equal(t, 0, client.getSpecificTeammateCalls, "GetSpecificTeammate should not be called when scopes are excluded from the sync filter")
	require.Equal(t, 0, client.getTeammatesSubAccessCalls, "GetTeammatesSubAccess should not be called when subusers are excluded from the sync filter")
}

func TestTeammateBuilderGrants_ScopesOnlySubusersExcluded(t *testing.T) {
	client := &fakeSendGridClient{}
	builder := newTeammateBuilder(client, true, false)
	resource := testTeammateResource(t)

	grants, _, err := builder.Grants(context.Background(), resource, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.True(t, isScopeGrant(grants[0]), "expected the single grant to be a scope grant")
	require.Equal(t, 0, client.getTeammatesSubAccessCalls)
}

func TestTeammateBuilderGrants_SubusersOnlyScopesExcluded(t *testing.T) {
	client := &fakeSendGridClient{}
	builder := newTeammateBuilder(client, false, true)
	resource := testTeammateResource(t)

	grants, _, err := builder.Grants(context.Background(), resource, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.True(t, isSubuserGrant(grants[0]), "expected the single grant to be a subuser grant")
	require.Equal(t, 0, client.getSpecificTeammateCalls)
}
