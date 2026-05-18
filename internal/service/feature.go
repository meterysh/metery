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

func (s *Service) CreateFeature(ctx context.Context, req *connect.Request[meteryv1.CreateFeatureRequest]) (*connect.Response[meteryv1.CreateFeatureResponse], error) {
	var meterID *string
	var meterSlug string
	if req.Msg.MeterSlug != "" {
		m, err := s.store.GetMeter(ctx, req.Msg.MeterSlug)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("meter not found"))
		}
		meterID = &m.ID
		meterSlug = m.Slug
	}

	f := &store.Feature{
		ID:        store.NewULID(),
		Slug:      req.Msg.Slug,
		Name:      req.Msg.Name,
		MeterID:   meterID,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.store.CreateFeature(ctx, f); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&meteryv1.CreateFeatureResponse{
		Feature: &meteryv1.Feature{
			Id:        f.ID,
			Slug:      f.Slug,
			Name:      f.Name,
			MeterSlug: meterSlug,
			CreatedAt: timestamppb.New(f.CreatedAt),
		},
	}), nil
}

func (s *Service) GetFeature(ctx context.Context, req *connect.Request[meteryv1.GetFeatureRequest]) (*connect.Response[meteryv1.GetFeatureResponse], error) {
	f, err := s.store.GetFeature(ctx, req.Msg.IdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("feature not found"))
	}
	return connect.NewResponse(&meteryv1.GetFeatureResponse{Feature: s.featureToProto(ctx, f)}), nil
}

func (s *Service) ListFeatures(ctx context.Context, req *connect.Request[meteryv1.ListFeaturesRequest]) (*connect.Response[meteryv1.ListFeaturesResponse], error) {
	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	fs, err := s.store.ListFeatures(ctx, req.Msg.IncludeArchived, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Feature, len(fs))
	for i := range fs {
		res[i] = s.featureToProto(ctx, &fs[i])
	}
	return connect.NewResponse(&meteryv1.ListFeaturesResponse{Features: res}), nil
}

func (s *Service) ArchiveFeature(ctx context.Context, req *connect.Request[meteryv1.ArchiveFeatureRequest]) (*connect.Response[meteryv1.ArchiveFeatureResponse], error) {
	if err := s.store.ArchiveFeature(ctx, req.Msg.IdOrSlug); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.ArchiveFeatureResponse{}), nil
}

func (s *Service) featureToProto(ctx context.Context, f *store.Feature) *meteryv1.Feature {
	r := &meteryv1.Feature{
		Id:        f.ID,
		Slug:      f.Slug,
		Name:      f.Name,
		CreatedAt: timestamppb.New(f.CreatedAt),
	}
	if f.MeterID != nil {
		if m, err := s.store.GetMeter(ctx, *f.MeterID); err == nil {
			r.MeterSlug = m.Slug
		}
	}
	if f.ArchivedAt != nil {
		r.ArchivedAt = timestamppb.New(*f.ArchivedAt)
	}
	return r
}
