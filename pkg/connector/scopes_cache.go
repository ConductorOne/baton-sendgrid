package connector

import (
	"context"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"
	"github.com/conductorone/baton-sdk/pkg/session"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
)

const teammateScopePrefix = "teammate-scopes/"

type scopeCache struct{}

func newScopeCache() *scopeCache {
	return &scopeCache{}
}

func (s *scopeCache) GetUsersForScope(ctx context.Context, ss sessions.SessionStore, scope string) ([]*models.TeammateScope, error) {
	all, err := session.GetAllJSON[*models.TeammateScope](ctx, ss, sessions.WithPrefix(teammateScopePrefix))
	if err != nil {
		return nil, err
	}

	var rv []*models.TeammateScope
	for _, teammate := range all {
		for _, ts := range teammate.Scopes {
			if ts == scope {
				rv = append(rv, teammate)
				break
			}
		}
	}

	return rv, nil
}
