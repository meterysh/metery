package service

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)


func (s *Service) CreateMeter(ctx context.Context, req *connect.Request[meteryv1.CreateMeterRequest]) (*connect.Response[meteryv1.CreateMeterResponse], error) {
	var vpPtr *string
	if req.Msg.ValueProperty != "" {
		v := req.Msg.ValueProperty
		vpPtr = &v
	}
	m := &store.Meter{
		ID:            store.NewULID(),
		Slug:          req.Msg.Slug,
		Name:          req.Msg.Name,
		Aggregation:   req.Msg.Aggregation,
		EventType:     req.Msg.EventType,
		ValueProperty: vpPtr,
		CreatedAt:     time.Now().UTC().Truncate(time.Second),
	}
	if err := s.store.CreateMeter(ctx, m); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	respMeter := &meteryv1.Meter{
		Id:          m.ID,
		Slug:        m.Slug,
		Name:        m.Name,
		Aggregation: m.Aggregation,
		EventType:   m.EventType,
		CreatedAt:   timestamppb.New(m.CreatedAt),
	}
	if m.ValueProperty != nil {
		respMeter.ValueProperty = *m.ValueProperty
	}

	return connect.NewResponse(&meteryv1.CreateMeterResponse{
		Meter: respMeter,
	}), nil
}

func (s *Service) GetMeter(ctx context.Context, req *connect.Request[meteryv1.GetMeterRequest]) (*connect.Response[meteryv1.GetMeterResponse], error) {
	m, err := s.store.GetMeter(ctx, req.Msg.IdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("meter not found"))
	}
	return connect.NewResponse(&meteryv1.GetMeterResponse{Meter: meterToProto(m)}), nil
}

func (s *Service) ListMeters(ctx context.Context, req *connect.Request[meteryv1.ListMetersRequest]) (*connect.Response[meteryv1.ListMetersResponse], error) {
	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	ms, err := s.store.ListMeters(ctx, req.Msg.IncludeArchived, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Meter, len(ms))
	for i := range ms {
		res[i] = meterToProto(&ms[i])
	}
	return connect.NewResponse(&meteryv1.ListMetersResponse{Meters: res}), nil
}

func (s *Service) ArchiveMeter(ctx context.Context, req *connect.Request[meteryv1.ArchiveMeterRequest]) (*connect.Response[meteryv1.ArchiveMeterResponse], error) {
	if err := s.store.ArchiveMeter(ctx, req.Msg.IdOrSlug); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.ArchiveMeterResponse{}), nil
}

func meterToProto(m *store.Meter) *meteryv1.Meter {
	r := &meteryv1.Meter{
		Id:          m.ID,
		Slug:        m.Slug,
		Name:        m.Name,
		Aggregation: m.Aggregation,
		EventType:   m.EventType,
		CreatedAt:   timestamppb.New(m.CreatedAt),
	}
	if m.ValueProperty != nil {
		r.ValueProperty = *m.ValueProperty
	}
	if m.ArchivedAt != nil {
		r.ArchivedAt = timestamppb.New(*m.ArchivedAt)
	}
	return r
}
