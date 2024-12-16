package connector

import (
	"context"

	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type scopeCache struct {
	client      client.SendGridClient
	scopeToUser map[string][]*client.TeammateScope
}

func newScopeCache(gridClient client.SendGridClient) *scopeCache {
	return &scopeCache{
		client:      gridClient,
		scopeToUser: make(map[string][]*client.TeammateScope),
	}
}

func (s *scopeCache) buildCache(ctx context.Context) error {
	l := ctxzap.Extract(ctx)

	l.Info("Building cache for scopes")

	s.scopeToUser = make(map[string][]*client.TeammateScope)
	teammates, err := s.client.GetTeammates(ctx)
	if err != nil {
		return err
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

	return nil
}

func (s *scopeCache) GetUsersForScope(scope string) []*client.TeammateScope {
	users, ok := s.scopeToUser[scope]

	if ok {
		return users
	}

	return []*client.TeammateScope{}
}
