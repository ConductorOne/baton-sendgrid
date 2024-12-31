package connector

import (
	"context"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	"github.com/conductorone/baton-sdk/pkg/pagination"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type scopeCache struct {
	client      SendGridClient
	scopeToUser map[string][]*models.TeammateScope
}

func newScopeCache(gridClient SendGridClient) *scopeCache {
	return &scopeCache{
		client:      gridClient,
		scopeToUser: make(map[string][]*models.TeammateScope),
	}
}

func (s *scopeCache) buildCache(ctx context.Context) error {
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

	return nil
}

func (s *scopeCache) GetUsersForScope(scope string) []*models.TeammateScope {
	users, ok := s.scopeToUser[scope]

	if ok {
		return users
	}

	return []*models.TeammateScope{}
}
