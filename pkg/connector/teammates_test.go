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
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeSessionStore is a minimal in-memory sessions.SessionStore, sufficient
// to exercise the Get/Set pairs teammateBuilder uses to dedupe across
// separate List() calls within a single sync.
type fakeSessionStore struct {
	data map[string][]byte
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{data: map[string][]byte{}}
}

func (f *fakeSessionStore) Get(_ context.Context, key string, _ ...sessions.SessionStoreOption) ([]byte, bool, error) {
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeSessionStore) GetMany(_ context.Context, keys []string, _ ...sessions.SessionStoreOption) (map[string][]byte, []string, error) {
	found := map[string][]byte{}
	var missing []string
	for _, k := range keys {
		if v, ok := f.data[k]; ok {
			found[k] = v
		} else {
			missing = append(missing, k)
		}
	}
	return found, missing, nil
}

func (f *fakeSessionStore) Set(_ context.Context, key string, value []byte, _ ...sessions.SessionStoreOption) error {
	f.data[key] = value
	return nil
}

func (f *fakeSessionStore) SetMany(_ context.Context, values map[string][]byte, _ ...sessions.SessionStoreOption) error {
	for k, v := range values {
		f.data[k] = v
	}
	return nil
}

func (f *fakeSessionStore) Delete(_ context.Context, key string, _ ...sessions.SessionStoreOption) error {
	delete(f.data, key)
	return nil
}

func (f *fakeSessionStore) Clear(_ context.Context, _ ...sessions.SessionStoreOption) error {
	f.data = map[string][]byte{}
	return nil
}

func (f *fakeSessionStore) GetAll(_ context.Context, _ string, _ ...sessions.SessionStoreOption) (map[string][]byte, string, error) {
	return f.data, "", nil
}

// fakeSendGridClient is a minimal, page_size=1-forcing implementation of
// SendGridClient used to exercise teammateBuilder.List's two flows (no
// parent vs. a subuser parent) without hitting the real SendGrid API.
type fakeSendGridClient struct {
	SendGridClient

	globalTeammates []*models.Teammate
	subusers        []models.Subuser
	// subuserTeammates maps subuser username -> teammates visible to it via on-behalf-of.
	subuserTeammates map[string][]*models.Teammate

	subAccess    []*models.TeammateSubuser
	subAccessErr error
}

func (f *fakeSendGridClient) GetTeammatesSubAccess(_ context.Context, _ sgclient.Username, pToken *pagination.Token, _ sgclient.OnBehalfOf) ([]*models.TeammateSubuser, string, error) {
	if f.subAccessErr != nil {
		return nil, "", f.subAccessErr
	}
	return pageOneAtATime(f.subAccess, pToken)
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
func (f *fakeSendGridClient) GetSpecificTeammate(_ context.Context, username sgclient.Username, onBehalfOf sgclient.OnBehalfOf) (*models.TeammateScope, error) {
	for _, tm := range f.globalTeammates {
		if tm.Username == string(username) {
			return &models.TeammateScope{Teammate: *tm}, nil
		}
	}
	for _, tm := range f.subuserTeammates[string(onBehalfOf)] {
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
// pagination terminates, guarding against an infinite loop. session may be
// nil (matching production calls where no session is configured) or shared
// across multiple drainTeammateList calls to simulate cross-subuser session
// state within one sync.
func drainTeammateList(t *testing.T, tb *teammateBuilder, parentResourceID *v2.ResourceId, session sessions.SessionStore) []*v2.Resource {
	t.Helper()

	var all []*v2.Resource
	token := ""
	for i := 0; i < 50; i++ {
		resources, results, err := tb.List(context.Background(), parentResourceID, rs.SyncOpAttrs{PageToken: pagination.Token{Token: token}, Session: session})
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

	tb := newTeammateBuilder(client, false)
	resources := drainTeammateList(t, tb, nil, nil)

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

	tb := newTeammateBuilder(client, false)

	sub1ResourceID, err := rs.NewResourceID(subuserResourceType, 1)
	require.NoError(t, err)

	resources := drainTeammateList(t, tb, sub1ResourceID, nil)

	require.Len(t, resources, 1, "global-admin must be deduped, only local-1 should be emitted")
	require.Equal(t, "local-1", resources[0].Id.Resource)
	require.NotNil(t, resources[0].ParentResourceId)
	require.Equal(t, subuserResourceType.Id, resources[0].ParentResourceId.ResourceType)
	require.Equal(t, "1", resources[0].ParentResourceId.Resource)
}

// TestTeammateBuilder_List_TeammateRestrictedToMultipleSubusers covers a
// teammate whose subuser_access spans a group of Subusers (a documented
// SendGrid feature — see GetTeammateSubuserAccess), rather than the parent
// account or a single Subuser. Without dedup across subusers within the
// sync, this teammate's username would be emitted once per subuser with the
// same resource ID but a different ParentResourceId, producing conflicting
// resources.
func TestTeammateBuilder_List_TeammateRestrictedToMultipleSubusers(t *testing.T) {
	client := &fakeSendGridClient{
		subusers: []models.Subuser{
			{Id: 1, Username: "sub1", Email: "sub1@example.com"},
			{Id: 2, Username: "sub2", Email: "sub2@example.com"},
		},
		subuserTeammates: map[string][]*models.Teammate{
			// restricted-1 is not a parent-scope teammate, but has access to
			// both sub1 and sub2 (subuser_access lists more than one).
			"sub1": {
				{Username: "restricted-1", Email: "restricted-1@example.com"},
			},
			"sub2": {
				{Username: "restricted-1", Email: "restricted-1@example.com"},
			},
		},
	}

	tb := newTeammateBuilder(client, false)
	session := newFakeSessionStore()

	sub1ResourceID, err := rs.NewResourceID(subuserResourceType, 1)
	require.NoError(t, err)
	sub2ResourceID, err := rs.NewResourceID(subuserResourceType, 2)
	require.NoError(t, err)

	sub1Resources := drainTeammateList(t, tb, sub1ResourceID, session)
	sub2Resources := drainTeammateList(t, tb, sub2ResourceID, session)

	require.Len(t, sub1Resources, 1, "restricted-1 should be emitted under the first subuser encountered")
	require.Equal(t, "restricted-1", sub1Resources[0].Id.Resource)
	require.Equal(t, "1", sub1Resources[0].ParentResourceId.Resource)

	require.Empty(t, sub2Resources, "restricted-1 must not be re-emitted under a second subuser with a conflicting ParentResourceId")
}

func TestTeammateBuilder_Grants_SubuserAccessForbidden(t *testing.T) {
	client := &fakeSendGridClient{
		subusers: []models.Subuser{
			{Id: 1, Username: "sub1", Email: "sub1@example.com"},
		},
		subuserTeammates: map[string][]*models.Teammate{
			"sub1": {
				{Username: "local-1", Email: "local-1@example.com"},
			},
		},
		subAccessErr: status.Error(codes.PermissionDenied, "403 Forbidden"),
	}

	sub1ResourceID, err := rs.NewResourceID(subuserResourceType, 1)
	require.NoError(t, err)

	resource, err := teammateResource(&models.Teammate{Username: "local-1", Email: "local-1@example.com"}, sub1ResourceID, "sub1")
	require.NoError(t, err)

	tb := newTeammateBuilder(client, false)
	grants, results, err := tb.Grants(context.Background(), resource, rs.SyncOpAttrs{PageToken: pagination.Token{}})

	require.NoError(t, err, "a 403 from subuser_access must not abort Grants for a subuser-only teammate")
	require.Empty(t, grants)
	require.Equal(t, "", results.NextPageToken)
}

func TestTeammateBuilder_Grants_SubuserAccessOtherErrorPropagates(t *testing.T) {
	client := &fakeSendGridClient{
		subusers: []models.Subuser{
			{Id: 1, Username: "sub1", Email: "sub1@example.com"},
		},
		subuserTeammates: map[string][]*models.Teammate{
			"sub1": {
				{Username: "local-1", Email: "local-1@example.com"},
			},
		},
		subAccessErr: status.Error(codes.Unavailable, "upstream unavailable"),
	}

	sub1ResourceID, err := rs.NewResourceID(subuserResourceType, 1)
	require.NoError(t, err)

	resource, err := teammateResource(&models.Teammate{Username: "local-1", Email: "local-1@example.com"}, sub1ResourceID, "sub1")
	require.NoError(t, err)

	tb := newTeammateBuilder(client, false)
	_, _, err = tb.Grants(context.Background(), resource, rs.SyncOpAttrs{PageToken: pagination.Token{}})

	require.Error(t, err, "only PermissionDenied should be tolerated, other errors must still propagate")
}

// scopeCallRecorder records whether the scope lookup was attempted.
type scopeCallRecorder struct {
	fakeSendGridClient
	called bool
}

func (f *scopeCallRecorder) GetSpecificTeammate(_ context.Context, _ sgclient.Username, _ sgclient.OnBehalfOf) (*models.TeammateScope, error) {
	f.called = true
	return &models.TeammateScope{Teammate: models.Teammate{Username: "u1"}, Scopes: []string{"mail.send"}}, nil
}

// Scope grants are cross-type. When scope is excluded from the sync filter the
// connector must not even make the per-teammate scope lookup. Subuser grants
// are unaffected: teammates own those.
func TestTeammateBuilder_Grants_SkipScopeResourceType(t *testing.T) {
	ctx := context.Background()
	res, err := teammateResource(&models.Teammate{Username: "u1", Email: "u1@example.com"}, nil, "")
	require.NoError(t, err)

	filtered := &scopeCallRecorder{}
	tb := newTeammateBuilder(filtered, true)
	grants, _, err := tb.Grants(ctx, res, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.False(t, filtered.called, "scope lookup must be skipped when scope is filtered out")
	for _, g := range grants {
		require.NotEqual(t, scopeResourceType.Id, g.GetEntitlement().GetResource().GetId().GetResourceType())
	}

	inScope := &scopeCallRecorder{}
	tb = newTeammateBuilder(inScope, false)
	_, _, err = tb.Grants(ctx, res, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.True(t, inScope.called, "scope lookup must run when scope is in the sync filter")
}
