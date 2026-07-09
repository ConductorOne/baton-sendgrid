package connector

import (
	"context"
	"fmt"
	"strings"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/pagination"
	rs "github.com/conductorone/baton-sdk/pkg/types/resource"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	listSubusersPhase  = "list-subusers"
	listTeammatesPhase = "list-teammates"
)

type subuserTeammateBuilder struct {
	resourceType *v2.ResourceType
	client       SendGridClient
}

func newSubuserTeammateBuilder(c SendGridClient) *subuserTeammateBuilder {
	return &subuserTeammateBuilder{
		resourceType: subuserTeammateResourceType,
		client:       c,
	}
}

func (r *subuserTeammateBuilder) ResourceType(ctx context.Context) *v2.ResourceType {
	return subuserTeammateResourceType
}

func (r *subuserTeammateBuilder) List(ctx context.Context, parentResourceID *v2.ResourceId, opts rs.SyncOpAttrs) ([]*v2.Resource, *rs.SyncOpResults, error) {
	bag := &pagination.Bag{}
	err := bag.Unmarshal(opts.PageToken.Token)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to unmarshal pagination token: %w", err)
	}

	if bag.Current() == nil {
		bag.Push(pagination.PageState{
			ResourceTypeID: listSubusersPhase,
			Token:          "0",
		})
	}

	current := bag.Current()

	if current.ResourceTypeID == listSubusersPhase {
		return r.listSubusersPhase(ctx, bag, current)
	}

	return r.listTeammatesPhase(ctx, bag, current)
}

func (r *subuserTeammateBuilder) listSubusersPhase(
	ctx context.Context,
	bag *pagination.Bag,
	current *pagination.PageState,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	l := ctxzap.Extract(ctx)

	pToken := &pagination.Token{Token: current.Token}
	subusers, nextToken, err := r.client.GetSubusers(ctx, pToken)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to get subusers: %w", err)
	}

	bag.Pop()

	if nextToken != "" && len(subusers) > 0 {
		bag.Push(pagination.PageState{
			ResourceTypeID: listSubusersPhase,
			Token:          nextToken,
		})
	}

	for i := len(subusers) - 1; i >= 0; i-- {
		bag.Push(pagination.PageState{
			ResourceTypeID: listTeammatesPhase,
			ResourceID:     encodeSubuserRef(subusers[i].Id, subusers[i].Username),
			Token:          "0",
		})
	}

	l.Debug("baton-sendgrid: queued subuser teammate lookups", zap.Int("subuser_count", len(subusers)))

	nextPageToken, err := bag.Marshal()
	if err != nil {
		return nil, nil, err
	}

	return nil, &rs.SyncOpResults{NextPageToken: nextPageToken}, nil
}

func (r *subuserTeammateBuilder) listTeammatesPhase(
	ctx context.Context,
	bag *pagination.Bag,
	current *pagination.PageState,
) ([]*v2.Resource, *rs.SyncOpResults, error) {
	l := ctxzap.Extract(ctx)

	subuserID, subuserUsername, err := decodeSubuserRef(current.ResourceID)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: invalid subuser ref in pagination state: %w", err)
	}

	pToken := &pagination.Token{Token: current.Token}
	teammates, nextTeammateToken, err := r.client.GetSubuserTeammates(ctx, subuserUsername, pToken)
	if err != nil {
		l.Warn("baton-sendgrid: failed to get teammates for subuser, skipping",
			zap.String("subuser", subuserUsername), zap.Error(err))

		if err := bag.Next(""); err != nil {
			return nil, nil, err
		}
		nextPageToken, err := bag.Marshal()
		if err != nil {
			return nil, nil, err
		}
		return nil, &rs.SyncOpResults{NextPageToken: nextPageToken}, nil
	}

	parentResourceID, err := rs.NewResourceID(subuserResourceType, subuserID)
	if err != nil {
		return nil, nil, fmt.Errorf("baton-sendgrid: failed to create parent resource ID: %w", err)
	}

	var rv []*v2.Resource
	for _, teammate := range teammates {
		resource, err := subuserTeammateResource(&teammate, subuserUsername, parentResourceID)
		if err != nil {
			return nil, nil, err
		}
		rv = append(rv, resource)
	}

	if nextTeammateToken != "" && len(teammates) > 0 {
		err = bag.Next(nextTeammateToken)
	} else {
		err = bag.Next("")
	}
	if err != nil {
		return nil, nil, err
	}

	nextPageToken, err := bag.Marshal()
	if err != nil {
		return nil, nil, err
	}

	l.Debug("baton-sendgrid: listed subuser teammates",
		zap.String("subuser", subuserUsername), zap.Int("count", len(rv)))

	return rv, &rs.SyncOpResults{NextPageToken: nextPageToken}, nil
}

func encodeSubuserRef(id int, username string) string {
	return fmt.Sprintf("%d\t%s", id, username)
}

func decodeSubuserRef(ref string) (int, string, error) {
	parts := strings.SplitN(ref, "\t", 2)
	if len(parts) != 2 {
		return 0, "", fmt.Errorf("invalid subuser ref format: %s", ref)
	}
	id := 0
	if _, err := fmt.Sscanf(parts[0], "%d", &id); err != nil {
		return 0, "", fmt.Errorf("invalid subuser id: %w", err)
	}
	return id, parts[1], nil
}

func (r *subuserTeammateBuilder) Entitlements(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Entitlement, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}

func (r *subuserTeammateBuilder) Grants(_ context.Context, _ *v2.Resource, _ rs.SyncOpAttrs) ([]*v2.Grant, *rs.SyncOpResults, error) {
	return nil, &rs.SyncOpResults{}, nil
}
