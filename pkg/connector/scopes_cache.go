package connector

import (
	"context"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/conductorone/baton-sdk/pkg/session"
	"github.com/conductorone/baton-sdk/pkg/types/sessions"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

const scopeCachePrefix = "scope-cache/"

type scopeCache struct {
	client SendGridClient
}

func newScopeCache(gridClient SendGridClient) *scopeCache {
	return &scopeCache{
		client: gridClient,
	}
}

func (s *scopeCache) buildCache(ctx context.Context, ss sessions.SessionStore) error {
	l := ctxzap.Extract(ctx)

	l.Info("Building cache for scopes")

	scopeToUser := make(map[string][]*models.TeammateScope)

	pToken := "0"

	for pToken != "" {
		var (
			teammates []models.Teammate
			err       error
		)

		teammates, pToken, err = s.client.GetTeammates(ctx, &pagination.Token{Token: pToken})
		if err != nil {
			return err
		}

		if len(teammates) == 0 {
			break
		}

		for _, teammate := range teammates {
			specificTeammate, err := s.client.GetSpecificTeammate(ctx, teammate.Username)
			if err != nil {
				return err
			}

			for _, scope := range specificTeammate.Scopes {
				scopeToUser[scope] = append(scopeToUser[scope], specificTeammate)
			}
		}
	}

	l.Info("Cache built for scopes")

	return session.SetManyJSON(ctx, ss, scopeToUser, sessions.WithPrefix(scopeCachePrefix))
}

func (s *scopeCache) GetUsersForScope(ctx context.Context, ss sessions.SessionStore, scope string) ([]*models.TeammateScope, error) {
	users, found, err := session.GetJSON[[]*models.TeammateScope](ctx, ss, scope, sessions.WithPrefix(scopeCachePrefix))
	if err != nil {
		return nil, err
	}
	if !found {
		return []*models.TeammateScope{}, nil
	}
	return users, nil
}
