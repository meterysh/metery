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
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) ListMeters(ctx context.Context, req *connect.Request[meteryv1.ListMetersRequest]) (*connect.Response[meteryv1.ListMetersResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) ArchiveMeter(ctx context.Context, req *connect.Request[meteryv1.ArchiveMeterRequest]) (*connect.Response[meteryv1.ArchiveMeterResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}
