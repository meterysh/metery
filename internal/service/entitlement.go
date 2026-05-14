package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/ledger"
	"github.com/meterysh/metery/internal/store"
)

func (s *Service) CreateEntitlement(ctx context.Context, req *connect.Request[meteryv1.CreateEntitlementRequest]) (*connect.Response[meteryv1.CreateEntitlementResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	f, err := s.store.GetFeature(ctx, req.Msg.FeatureIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feature not found"))
	}

	var dur *string
	if req.Msg.UsagePeriod != nil {
		d := req.Msg.UsagePeriod.Duration
		dur = &d
	}

	e := &store.EntitlementRow{
		ID:                  store.NewULID(),
		CustomerID:          c.ID,
		FeatureID:           f.ID,
		UsagePeriodDuration: dur,
		CreatedAt:           time.Now().UTC().Truncate(time.Second),
	}

	if err := s.store.CreateEntitlement(ctx, e); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&meteryv1.CreateEntitlementResponse{
		Entitlement: &meteryv1.Entitlement{
			Id:          e.ID,
			FeatureSlug: f.Slug,
		},
	}), nil
}

func (s *Service) GetEntitlement(ctx context.Context, req *connect.Request[meteryv1.GetEntitlementRequest]) (*connect.Response[meteryv1.GetEntitlementResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) ListEntitlements(ctx context.Context, req *connect.Request[meteryv1.ListEntitlementsRequest]) (*connect.Response[meteryv1.ListEntitlementsResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) DeleteEntitlement(ctx context.Context, req *connect.Request[meteryv1.DeleteEntitlementRequest]) (*connect.Response[meteryv1.DeleteEntitlementResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) GetEntitlementValue(ctx context.Context, req *connect.Request[meteryv1.GetEntitlementValueRequest]) (*connect.Response[meteryv1.GetEntitlementValueResponse], error) {
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
		if errors.Is(err, sql.ErrNoRows) {
			return connect.NewResponse(&meteryv1.GetEntitlementValueResponse{
				Value: &meteryv1.EntitlementValue{HasAccess: false},
			}), nil
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	if f.MeterID == nil {
		return connect.NewResponse(&meteryv1.GetEntitlementValueResponse{
			Value: &meteryv1.EntitlementValue{HasAccess: true},
		}), nil
	}

	m, err := s.store.GetMeter(ctx, *f.MeterID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	grants, err := s.store.ListActiveGrants(ctx, e.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	evalTime := time.Now().UTC().Truncate(time.Second)
	if req.Msg.At != nil {
		evalTime = req.Msg.At.AsTime()
	}

	fetchUsage := func(from, to time.Time) int64 {
		usage, _ := s.store.FetchUsage(ctx, c.Key, m, from, to)
		return usage
	}

	domainEnt := ledger.Entitlement{
		ID:                  e.ID,
		UsagePeriodDuration: e.UsagePeriodDuration,
		UsagePeriodAnchor:   e.UsagePeriodAnchor,
		CreatedAt:           e.CreatedAt,
	}

	res := ledger.CalculateBalance(evalTime, domainEnt, grants, fetchUsage)

	respValue := &meteryv1.EntitlementValue{
		HasAccess: res.HasAccess,
		Balance:   &res.Balance,
		Usage:     &res.Usage,
		Overage:   &res.Overage,
	}

	if req.Msg.Cost != nil {
		if res.Balance < *req.Msg.Cost {
			respValue.HasAccess = false
		}
	}

	return connect.NewResponse(&meteryv1.GetEntitlementValueResponse{
		Value: respValue,
	}), nil
}

func (s *Service) ResetEntitlement(ctx context.Context, req *connect.Request[meteryv1.ResetEntitlementRequest]) (*connect.Response[meteryv1.ResetEntitlementResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}
