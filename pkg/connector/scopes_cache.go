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
	client      SendGridClient
	scopeToUser map[string][]*models.TeammateScope
	loaded      bool
}

func newScopeCache(gridClient SendGridClient) *scopeCache {
	return &scopeCache{
		client:      gridClient,
		scopeToUser: make(map[string][]*models.TeammateScope),
	}
}

func (s *scopeCache) buildCache(ctx context.Context, ss sessions.SessionStore) error {
	l := ctxzap.Extract(ctx)

	l.Info("Building cache for scopes")

	s.scopeToUser = make(map[string][]*models.TeammateScope)

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
				s.scopeToUser[scope] = append(s.scopeToUser[scope], specificTeammate)
			}
		}
	}

	l.Info("Cache built for scopes")

	s.loaded = true

	if ss != nil {
		if err := session.SetManyJSON(ctx, ss, s.scopeToUser, sessions.WithPrefix(scopeCachePrefix)); err != nil {
			return err
		}
	}

	return nil
}

func (s *scopeCache) GetUsersForScope(ctx context.Context, ss sessions.SessionStore, scope string) ([]*models.TeammateScope, error) {
	if !s.loaded && ss != nil {
		cached, err := session.GetAllJSON[[]*models.TeammateScope](ctx, ss, sessions.WithPrefix(scopeCachePrefix))
		if err != nil {
			return nil, err
		}
		s.scopeToUser = cached
		s.loaded = true
	}

	users, ok := s.scopeToUser[scope]
	if ok {
		return users, nil
	}

	return []*models.TeammateScope{}, nil
}
