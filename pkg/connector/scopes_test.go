package connector

import (
	"context"
	"slices"
	"testing"

	sgclient "github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// scopeStateFake models a teammate's scopes in SendGrid together with the http
// cache sitting in front of the read: GetSpecificTeammate replays the first
// response it ever produced (uhttp caches GETs for an hour and never
// invalidates them on a write), while GetSpecificTeammateNoCache always
// reports live state. SetTeammateScopes replaces the entire list, as the real
// PATCH does — so a provisioning read served from cache silently drops
// whatever the previous task wrote.
type scopeStateFake struct {
	fakeSendGridClient

	live         []string
	cached       []string
	cachedFilled bool
	// cachedReads counts reads that went through the cacheable variant, which
	// no provisioning path may use.
	cachedReads int

	// scopes/scopesErr drive the builder's own GetScopes method (do NOT rely
	// on the embedded fake's fields).
	scopes    []string
	scopesErr error
}

// GetScopes backs scopeBuilder.List's catalog fetch; owning the method (not
// the embedded interface) lets List tests drive the catalog directly.
func (f *scopeStateFake) GetScopes(_ context.Context) ([]string, error) {
	if f.scopesErr != nil {
		return nil, f.scopesErr
	}
	return f.scopes, nil
}

func (f *scopeStateFake) GetSpecificTeammate(_ context.Context, username sgclient.Username, _ sgclient.OnBehalfOf) (*models.TeammateScope, error) {
	f.cachedReads++
	if !f.cachedFilled {
		f.cached = slices.Clone(f.live)
		f.cachedFilled = true
	}

	return &models.TeammateScope{
		Teammate: models.Teammate{Username: string(username)},
		Scopes:   slices.Clone(f.cached),
	}, nil
}

func (f *scopeStateFake) GetSpecificTeammateNoCache(_ context.Context, username sgclient.Username, _ sgclient.OnBehalfOf) (*models.TeammateScope, error) {
	return &models.TeammateScope{
		Teammate: models.Teammate{Username: string(username)},
		Scopes:   slices.Clone(f.live),
	}, nil
}

func (f *scopeStateFake) SetTeammateScopes(_ context.Context, _ sgclient.Username, scopes []string, _ bool, _ sgclient.OnBehalfOf) error {
	f.live = slices.Clone(scopes)
	return nil
}

func scopeEntitlementFor(t *testing.T, scope string) *v2.Entitlement {
	t.Helper()

	scopeRs, err := scopeResource(Scope(scope))
	require.NoError(t, err)

	return &v2.Entitlement{Resource: scopeRs, Slug: assignedEntitlement}
}

func teammatePrincipal(t *testing.T) *v2.Resource {
	t.Helper()

	principal, err := teammateResource(
		&models.Teammate{Username: "alice@example.com", Email: "alice@example.com"},
		nil,
		"",
	)
	require.NoError(t, err)

	return principal
}

// Two grants for the same teammate arriving back-to-back: the second must read
// the scope list the first one wrote, not a pre-write snapshot, or it hands
// SetTeammateScopes a full list that's missing the first grant.
func TestScopeBuilder_Grant_ConsecutiveGrantsDoNotOverwriteEachOther(t *testing.T) {
	ctx := context.Background()
	client := &scopeStateFake{live: []string{"alerts.read", "mail.send"}}
	principal := teammatePrincipal(t)
	sb := newScopeBuilder(client)

	_, _, err := sb.Grant(ctx, principal, scopeEntitlementFor(t, "api_keys.read"))
	require.NoError(t, err)

	_, _, err = sb.Grant(ctx, principal, scopeEntitlementFor(t, "billing.read"))
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"alerts.read", "mail.send", "api_keys.read", "billing.read"}, client.live)
	require.Zero(t, client.cachedReads, "the read feeding SetTeammateScopes must bypass the http cache")
}

// Revoke reads the same way: it removes one scope from the live list, so a
// cached read would resurrect scopes revoked by an earlier task and drop ones
// granted by it.
func TestScopeBuilder_Revoke_ReadsLiveScopes(t *testing.T) {
	ctx := context.Background()
	client := &scopeStateFake{live: []string{"alerts.read", "mail.send"}}
	principal := teammatePrincipal(t)
	sb := newScopeBuilder(client)

	_, _, err := sb.Grant(ctx, principal, scopeEntitlementFor(t, "api_keys.read"))
	require.NoError(t, err)

	_, err = sb.Revoke(ctx, &v2.Grant{
		Principal:   principal,
		Entitlement: scopeEntitlementFor(t, "mail.send"),
	})
	require.NoError(t, err)

	require.ElementsMatch(t, []string{"alerts.read", "api_keys.read"}, client.live)
	require.Zero(t, client.cachedReads, "the read feeding SetTeammateScopes must bypass the http cache")
}

// The idempotency short-circuits decide off the same read, so they must also
// see live state: a scope granted by a previous task must be reported as
// already granted rather than re-written.
func TestScopeBuilder_Grant_AlreadyGrantedUsesLiveScopes(t *testing.T) {
	ctx := context.Background()
	client := &scopeStateFake{live: []string{"alerts.read"}}
	principal := teammatePrincipal(t)
	sb := newScopeBuilder(client)

	_, _, err := sb.Grant(ctx, principal, scopeEntitlementFor(t, "api_keys.read"))
	require.NoError(t, err)

	grants, annos, err := sb.Grant(ctx, principal, scopeEntitlementFor(t, "api_keys.read"))
	require.NoError(t, err)
	require.Empty(t, grants)
	require.True(t, annos.Contains(&v2.GrantAlreadyExists{}))
	require.ElementsMatch(t, []string{"alerts.read", "api_keys.read"}, client.live)
	require.Zero(t, client.cachedReads, "the read feeding SetTeammateScopes must bypass the http cache")
}

// TestScopeBuilder_List_UnionFloorAndLive proves the catalog is the sorted
// union of the immutable floor with the live GET /v3/scopes response (AC3,
// I2): a floor-only scope is still listed when live omits it, a live-only
// scope is added, output is sorted by scope string, and there are no
// duplicates.
func TestScopeBuilder_List_UnionFloorAndLive(t *testing.T) {
	client := &scopeStateFake{
		scopes: []string{"mail.send", "sso.settings.read", "alerts.read"}, // alerts.read is in the floor too
	}

	sb := newScopeBuilder(client)
	rv, results, err := sb.List(context.Background(), nil, rs.SyncOpAttrs{})
	require.NoError(t, err)
	require.NotNil(t, results)

	var got []string
	for _, r := range rv {
		require.Equal(t, scopeResourceType.Id, r.Id.ResourceType)
		require.Equal(t, string(Scope(r.Id.Resource)), r.Id.Resource, "scope resource ID must be the vendor scope string (I1)")
		got = append(got, r.Id.Resource)
	}

	require.Contains(t, got, "billing.create", "floor-only scope must survive the union")
	require.Contains(t, got, "sso.settings.read", "live-only scope must be added")
	require.Contains(t, got, "mail.send")

	require.True(t, slices.IsSorted(got), "catalog must be emitted sorted by scope string")

	seen := make(map[string]struct{})
	for _, s := range got {
		_, dup := seen[s]
		require.False(t, dup, "duplicate scope resource %s in catalog", s)
		seen[s] = struct{}{}
	}

	// The union must be a superset of the floor (additive-only, B-series).
	for scope := range SendGridScopes {
		require.Contains(t, seen, string(scope), "floor scope %s must still be listed", scope)
	}
}

// TestScopeBuilder_List_GetScopesErrorPropagates proves a GetScopes failure
// fails the sync: List returns a wrapped error, never empty-with-nil-error
// (S1/I4) and never a floor fallback.
func TestScopeBuilder_List_GetScopesErrorPropagates(t *testing.T) {
	client := &scopeStateFake{
		scopesErr: status.Error(codes.Unavailable, "v3/scopes is down"),
	}

	sb := newScopeBuilder(client)
	rv, _, err := sb.List(context.Background(), nil, rs.SyncOpAttrs{})

	require.Error(t, err)
	require.Contains(t, err.Error(), "baton-sendgrid: failed to get scope catalog")
	require.ErrorIs(t, err, client.scopesErr, "GetScopes error must be wrapped with %w, not replaced")
	require.Empty(t, rv)
}
