package connector

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-sendgrid/pkg/connector/models"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	ent "github.com/conductorone/baton-sdk/pkg/types/entitlement"
	"github.com/conductorone/baton-sdk/pkg/types/grant"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	accessEntitlement = "access"
)

type teammateBuilder struct {
	client SendGridClient
}

func (u *teammateBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return teammateResourceType
}

func (u *teammateBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, pToken *pagination.Token) ([]*v2.Resource, string, annotations.Annotations, error) {
	teammates, pNextToken, err := u.client.GetTeammates(ctx, pToken)
	if err != nil {
		return nil, "", nil, err
	}

	rv := make([]*v2.Resource, len(teammates))
	for i, teammate := range teammates {
		us, err := teammateResource(ctx, &teammate, nil)
		if err != nil {
			return nil, "", nil, err
		}
		rv[i] = us
	}

	nextToken := ""
	if len(teammates) != 0 {
		nextToken = pNextToken
	}

	return rv, nextToken, nil, nil
}

func (u *teammateBuilder) Entitlements(_ context.Context, resource *v2.Resource, _ *pagination.Token) ([]*v2.Entitlement, string, annotations.Annotations, error) {
	var rv []*v2.Entitlement
	assigmentOptions := []ent.EntitlementOption{
		ent.WithGrantableTo(subuserResourceType),
		ent.WithDescription(fmt.Sprintf("Teammate acess to %s", subuserResourceType.DisplayName)),
		ent.WithDisplayName(fmt.Sprintf("%s can access %s", resource.DisplayName, subuserResourceType.DisplayName)),
	}
	rv = append(rv, ent.NewAssignmentEntitlement(resource, accessEntitlement, assigmentOptions...))

	return rv, "", nil, nil
}

func (u *teammateBuilder) Grants(ctx context.Context, resource *v2.Resource, pToken *pagination.Token) ([]*v2.Grant, string, annotations.Annotations, error) {
	var rv []*v2.Grant

	username := resource.Id.Resource

	access, nextToken, err := u.client.GetTeammatesSubAccess(ctx, username, pToken)
	if err != nil {
		return nil, "", nil, err
	}

	logger := ctxzap.Extract(ctx)
	logger.Info("Teammate grants", zap.String("username", username), zap.Any("COUNT", access))

	for _, subAcess := range access {
		grants, err := createGrantSubuserFromTeammate(ctx, resource, &subAcess)
		if err != nil {
			return nil, "", nil, err
		}

		rv = append(rv, grants...)
	}

	return rv, nextToken, nil, nil
}

func newTeammateBuilder(client SendGridClient) *teammateBuilder {
	return &teammateBuilder{
		client: client,
	}
}

func createGrantSubuserFromTeammate(ctx context.Context, resource *v2.Resource, subAcess *models.TeammateSubuser) ([]*v2.Grant, error) {
	userId, err := rs.NewResourceID(subuserResourceType, subAcess.Id)
	if err != nil {
		return nil, err
	}

	rv := []*v2.Grant{
		grant.NewGrant(resource, accessEntitlement, userId),
	}

	return rv, nil
}
