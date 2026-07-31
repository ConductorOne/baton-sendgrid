package connector

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	sgclient "github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeSendGridClient is a minimal, page_size=1-forcing implementation of
// SendGridClient used to exercise teammateBuilder.List's two flows (no
// parent vs. a subuser parent) without hitting the real SendGrid API.
type fakeSendGridClient struct {
	SendGridClient

	globalTeammates []*models.Teammate
	subusers        []models.Subuser
	// subuserTeammates maps subuser username -> teammates visible to it via on-behalf-of.
	subuserTeammates map[string][]*models.Teammate
}

func (f *fakeSendGridClient) GetTeammates(_ context.Context, pToken *pagination.Token, onBehalfOf sgclient.OnBehalfOf) ([]*models.Teammate, string, error) {
	list := f.globalTeammates
	if onBehalfOf != "" {
		list = f.subuserTeammates[string(onBehalfOf)]
	}

	return pageOneAtATime(list, pToken)
}

func (f *fakeSendGridClient) GetSubusers(_ context.Context, pToken *pagination.Token) ([]models.Subuser, string, error) {
	return pageOneAtATime(f.subusers, pToken)
}

func (f *fakeSendGridClient) GetSubuserUsernameByID(_ context.Context, subuserID string) (string, error) {
	for _, su := range f.subusers {
		if strconv.Itoa(su.Id) == subuserID {
			return su.Username, nil
		}
	}
	return "", fmt.Errorf("subuser %s not found", subuserID)
}

// GetSpecificTeammate backs isParentScopeTeammate's dedup check: onBehalfOf
// "" means "does this username exist at parent scope", answered against
// globalTeammates, mirroring the real API's 404-for-missing behavior.
func (f *fakeSendGridClient) GetSpecificTeammate(_ context.Context, username sgclient.Username, _ sgclient.OnBehalfOf) (*models.TeammateScope, error) {
	for _, tm := range f.globalTeammates {
		if tm.Username == string(username) {
			return &models.TeammateScope{Teammate: *tm}, nil
		}
	}
	return nil, status.Error(codes.NotFound, "teammate does not exist")
}

// pageOneAtATime always returns exactly one item per call, forcing the same
// multi-page code path the review checklist requires validating (see
// build-pagination.md: "verified pagination works by temporarily setting
// page_size=1").
func pageOneAtATime[T any](items []T, pToken *pagination.Token) ([]T, string, error) {
	offset := 0
	if pToken.Token != "" {
		var err error
		offset, err = strconv.Atoi(pToken.Token)
		if err != nil {
			return nil, "", err
		}
	}

	if offset >= len(items) {
		return nil, "", nil
	}

	next := ""
	if offset+1 < len(items) {
		next = strconv.Itoa(offset + 1)
	}

	return items[offset : offset+1], next, nil
}

// drainTeammateList repeatedly calls List with the given parent (nil for the
// root/no-parent flow, or a subuser's ResourceId for the child flow) until
// pagination terminates, guarding against an infinite loop.
func drainTeammateList(t *testing.T, tb *teammateBuilder, parentResourceID *v2.ResourceId) []*v2.Resource {
	t.Helper()

	var all []*v2.Resource
	token := ""
	for i := 0; i < 50; i++ {
		resources, results, err := tb.List(context.Background(), parentResourceID, rs.SyncOpAttrs{PageToken: pagination.Token{Token: token}})
		require.NoError(t, err)
		all = append(all, resources...)

		if results.NextPageToken == "" {
			return all
		}
		require.NotEqual(t, token, results.NextPageToken, "next page token did not change — would infinite loop")
		token = results.NextPageToken
	}

	t.Fatal("teammateBuilder.List did not terminate within 50 pages")
	return nil
}

func TestTeammateBuilder_List_RootTeammates(t *testing.T) {
	client := &fakeSendGridClient{
		globalTeammates: []*models.Teammate{
			{Username: "global-admin", Email: "global-admin@example.com"},
			{Username: "global-viewer", Email: "global-viewer@example.com"},
		},
	}

	tb := newTeammateBuilder(client)
	resources := drainTeammateList(t, tb, nil)

	require.Len(t, resources, 2)
	for _, r := range resources {
		require.Nil(t, r.ParentResourceId, "root-flow teammates must not have a parent resource")
	}
}

func TestTeammateBuilder_List_SubuserTeammates(t *testing.T) {
	client := &fakeSendGridClient{
		globalTeammates: []*models.Teammate{
			{Username: "global-admin", Email: "global-admin@example.com"},
		},
		subusers: []models.Subuser{
			{Id: 1, Username: "sub1", Email: "sub1@example.com"},
		},
		subuserTeammates: map[string][]*models.Teammate{
			// global-admin also has access to sub1 — must NOT be re-emitted
			// under sub1's parent.
			"sub1": {
				{Username: "global-admin", Email: "global-admin@example.com"},
				{Username: "local-1", Email: "local-1@example.com"},
			},
		},
	}

	tb := newTeammateBuilder(client)

	sub1ResourceID, err := rs.NewResourceID(subuserResourceType, 1)
	require.NoError(t, err)

	resources := drainTeammateList(t, tb, sub1ResourceID)

	require.Len(t, resources, 1, "global-admin must be deduped, only local-1 should be emitted")
	require.Equal(t, "local-1", resources[0].Id.Resource)
	require.NotNil(t, resources[0].ParentResourceId)
	require.Equal(t, subuserResourceType.Id, resources[0].ParentResourceId.ResourceType)
	require.Equal(t, "1", resources[0].ParentResourceId.Resource)
}
