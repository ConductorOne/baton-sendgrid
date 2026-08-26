package connector

import (
	"context"
	"slices"
	"testing"

	sgclient "github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/stretchr/testify/require"
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
