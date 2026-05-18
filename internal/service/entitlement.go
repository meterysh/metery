package service

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/ledger"
	"github.com/meterysh/metery/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
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
		Entitlement: entitlementToProto(e, c.Key, f.Slug),
	}), nil
}

func (s *Service) GetEntitlement(ctx context.Context, req *connect.Request[meteryv1.GetEntitlementRequest]) (*connect.Response[meteryv1.GetEntitlementResponse], error) {
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
	return connect.NewResponse(&meteryv1.GetEntitlementResponse{
		Entitlement: entitlementToProto(e, c.Key, f.Slug),
	}), nil
}

func (s *Service) ListEntitlements(ctx context.Context, req *connect.Request[meteryv1.ListEntitlementsRequest]) (*connect.Response[meteryv1.ListEntitlementsResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	es, err := s.store.ListEntitlements(ctx, c.ID, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Entitlement, len(es))
	for i, e := range es {
		f, _ := s.store.GetFeature(ctx, e.FeatureID)
		slug := ""
		if f != nil {
			slug = f.Slug
		}
		res[i] = entitlementToProto(&e, c.Key, slug)
	}
	return connect.NewResponse(&meteryv1.ListEntitlementsResponse{Entitlements: res}), nil
}

func (s *Service) DeleteEntitlement(ctx context.Context, req *connect.Request[meteryv1.DeleteEntitlementRequest]) (*connect.Response[meteryv1.DeleteEntitlementResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	f, err := s.store.GetFeature(ctx, req.Msg.FeatureIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feature not found"))
	}
	if err := s.store.DeleteEntitlement(ctx, c.ID, f.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.DeleteEntitlementResponse{}), nil
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

	seed, _ := s.store.GetLatestSnapshot(ctx, e.ID, evalTime)

	res, newSnaps := ledger.CalculateBalance(evalTime, domainEnt, grants, fetchUsage, seed)

	if len(newSnaps) > 0 {
		entID := e.ID
		go func() {
			if err := s.store.SaveSnapshots(context.Background(), newSnaps, entID); err != nil {
				log.Printf("save balance snapshots for %s: %v", entID, err)
			}
		}()
	}

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
	at := time.Now().UTC().Truncate(time.Second)
	if req.Msg.At != nil {
		at = req.Msg.At.AsTime()
	}
	if err := s.store.ResetEntitlement(ctx, e.ID, at); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.ResetEntitlementResponse{}), nil
}

func entitlementToProto(e *store.EntitlementRow, customerKey, featureSlug string) *meteryv1.Entitlement {
	r := &meteryv1.Entitlement{
		Id:          e.ID,
		CustomerKey: customerKey,
		FeatureSlug: featureSlug,
		CreatedAt:   timestamppb.New(e.CreatedAt),
	}
	if e.UsagePeriodDuration != nil {
		r.UsagePeriod = &meteryv1.Period{Duration: *e.UsagePeriodDuration}
		if e.UsagePeriodAnchor != nil {
			r.UsagePeriod.Anchor = timestamppb.New(*e.UsagePeriodAnchor)
		}
	}
	if e.DeletedAt != nil {
		r.DeletedAt = timestamppb.New(*e.DeletedAt)
	}
	return r
}
