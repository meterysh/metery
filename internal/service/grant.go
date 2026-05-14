package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
	"github.com/sosodev/duration"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) CreateGrant(ctx context.Context, req *connect.Request[meteryv1.CreateGrantRequest]) (*connect.Response[meteryv1.CreateGrantResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	f, err := s.store.GetFeature(ctx, req.Msg.FeatureIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feature not found"))
	}
	e, err := s.store.GetEntitlement(ctx, c.ID, f.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("entitlement not found"))
	}

	priority := int32(100)
	if req.Msg.Priority != nil {
		priority = *req.Msg.Priority
	}

	effTime := time.Now().UTC().Truncate(time.Second)
	if req.Msg.EffectiveAt != nil {
		effTime = req.Msg.EffectiveAt.AsTime()
	}

	var metaStr *string
	if req.Msg.Metadata != nil {
		b, err := json.Marshal(req.Msg.Metadata.AsMap())
		if err == nil {
			s := string(b)
			metaStr = &s
		}
	}

	g := &store.GrantRow{
		ID:            store.NewULID(),
		EntitlementID: e.ID,
		Amount:        req.Msg.Amount,
		Priority:      priority,
		EffectiveAt:   effTime,
		Metadata:      metaStr,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if req.Msg.Expiration != nil {
		dur, err := duration.Parse(req.Msg.Expiration.Duration)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid expiration duration format"))
		}
		expTime := shiftTime(effTime, dur)
		g.ExpiresAt = &expTime
	}

	if err := s.store.CreateGrant(ctx, g); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	respGrant := &meteryv1.Grant{
		Id:          g.ID,
		Amount:      g.Amount,
		Priority:    g.Priority,
		EffectiveAt: timestamppb.New(g.EffectiveAt),
		CreatedAt:   timestamppb.New(g.CreatedAt),
		Metadata:    req.Msg.Metadata,
	}

	if g.ExpiresAt != nil {
		respGrant.ExpiresAt = timestamppb.New(*g.ExpiresAt)
	}

	return connect.NewResponse(&meteryv1.CreateGrantResponse{
		Grant: respGrant,
	}), nil
}

func (s *Service) ListGrants(ctx context.Context, req *connect.Request[meteryv1.ListGrantsRequest]) (*connect.Response[meteryv1.ListGrantsResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	f, err := s.store.GetFeature(ctx, req.Msg.FeatureIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feature not found"))
	}
	e, err := s.store.GetEntitlement(ctx, c.ID, f.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("entitlement not found"))
	}
	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	gs, err := s.store.ListGrants(ctx, e.ID, req.Msg.IncludeVoided, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Grant, len(gs))
	for i := range gs {
		res[i] = grantRowToProto(&gs[i])
	}
	return connect.NewResponse(&meteryv1.ListGrantsResponse{Grants: res}), nil
}

func (s *Service) VoidGrant(ctx context.Context, req *connect.Request[meteryv1.VoidGrantRequest]) (*connect.Response[meteryv1.VoidGrantResponse], error) {
	if err := s.store.VoidGrant(ctx, req.Msg.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.VoidGrantResponse{}), nil
}

func grantRowToProto(g *store.GrantRow) *meteryv1.Grant {
	r := &meteryv1.Grant{
		Id:            g.ID,
		EntitlementId: g.EntitlementID,
		Amount:        g.Amount,
		Priority:      g.Priority,
		EffectiveAt:   timestamppb.New(g.EffectiveAt),
		CreatedAt:     timestamppb.New(g.CreatedAt),
	}
	if g.ExpiresAt != nil {
		r.ExpiresAt = timestamppb.New(*g.ExpiresAt)
	}
	if g.VoidedAt != nil {
		r.VoidedAt = timestamppb.New(*g.VoidedAt)
	}
	if g.RecurrenceInterval != nil {
		r.Recurrence = &meteryv1.Recurrence{Interval: *g.RecurrenceInterval}
		if g.RecurrenceAnchor != nil {
			r.Recurrence.Anchor = timestamppb.New(*g.RecurrenceAnchor)
		}
	}
	if g.RolloverMax != nil && g.RolloverType != nil {
		r.Rollover = &meteryv1.Rollover{MaxAmount: *g.RolloverMax, Type: *g.RolloverType}
	}
	return r
}

func shiftTime(t time.Time, d *duration.Duration) time.Time {
	years := int(d.Years)
	months := int(d.Months)
	days := int(d.Weeks)*7 + int(d.Days)

	shifted := t.AddDate(years, months, days)

	hours := time.Duration(d.Hours) * time.Hour
	minutes := time.Duration(d.Minutes) * time.Minute
	seconds := time.Duration(d.Seconds) * time.Second

	return shifted.Add(hours + minutes + seconds)
}
