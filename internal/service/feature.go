package service

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
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
		},
	}), nil
}

func (s *Service) GetFeature(ctx context.Context, req *connect.Request[meteryv1.GetFeatureRequest]) (*connect.Response[meteryv1.GetFeatureResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) ListFeatures(ctx context.Context, req *connect.Request[meteryv1.ListFeaturesRequest]) (*connect.Response[meteryv1.ListFeaturesResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}

func (s *Service) ArchiveFeature(ctx context.Context, req *connect.Request[meteryv1.ArchiveFeatureRequest]) (*connect.Response[meteryv1.ArchiveFeatureResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("not implemented"))
}
