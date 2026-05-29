package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) CreatePlan(ctx context.Context, req *connect.Request[meteryv1.CreatePlanRequest]) (*connect.Response[meteryv1.CreatePlanResponse], error) {
	for _, e := range req.Msg.Entries {
		if _, err := s.store.GetFeature(ctx, e.FeatureSlug); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("feature not found: "+e.FeatureSlug))
		}
	}

	entriesJSON, err := planEntriesToJSON(req.Msg.Entries)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var metaStr *string
	if req.Msg.Metadata != nil {
		b, err := json.Marshal(req.Msg.Metadata.AsMap())
		if err == nil {
			s := string(b)
			metaStr = &s
		}
	}

	p := &store.PlanRow{
		ID:        store.NewULID(),
		Slug:      req.Msg.Slug,
		Name:      req.Msg.Name,
		Entries:   entriesJSON,
		Metadata:  metaStr,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if r := req.Msg.Recurrence; r != nil {
		iv := r.Interval
		p.RecurrenceInterval = &iv
		if r.Anchor != nil {
			t := r.Anchor.AsTime()
			p.RecurrenceAnchor = &t
		}
	}
	if err := s.store.CreatePlan(ctx, p); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	plan, err := planRowToProto(p)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.CreatePlanResponse{Plan: plan}), nil
}

func (s *Service) GetPlan(ctx context.Context, req *connect.Request[meteryv1.GetPlanRequest]) (*connect.Response[meteryv1.GetPlanResponse], error) {
	p, err := s.store.GetPlan(ctx, req.Msg.IdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("plan not found"))
	}
	plan, err := planRowToProto(p)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.GetPlanResponse{Plan: plan}), nil
}

func (s *Service) ListPlans(ctx context.Context, req *connect.Request[meteryv1.ListPlansRequest]) (*connect.Response[meteryv1.ListPlansResponse], error) {
	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	rows, err := s.store.ListPlans(ctx, req.Msg.IncludeArchived, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Plan, len(rows))
	for i := range rows {
		p, err := planRowToProto(&rows[i])
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		res[i] = p
	}
	return connect.NewResponse(&meteryv1.ListPlansResponse{Plans: res}), nil
}

func (s *Service) ArchivePlan(ctx context.Context, req *connect.Request[meteryv1.ArchivePlanRequest]) (*connect.Response[meteryv1.ArchivePlanResponse], error) {
	if err := s.store.ArchivePlan(ctx, req.Msg.IdOrSlug); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.ArchivePlanResponse{}), nil
}

func planRowToProto(p *store.PlanRow) (*meteryv1.Plan, error) {
	entries, err := planEntriesFromJSON(p.Entries)
	if err != nil {
		return nil, err
	}
	plan := &meteryv1.Plan{
		Id:        p.ID,
		Slug:      p.Slug,
		Name:      p.Name,
		Entries:   entries,
		CreatedAt: timestamppb.New(p.CreatedAt),
	}
	if p.RecurrenceInterval != nil {
		plan.Recurrence = &meteryv1.Recurrence{Interval: *p.RecurrenceInterval}
		if p.RecurrenceAnchor != nil {
			plan.Recurrence.Anchor = timestamppb.New(*p.RecurrenceAnchor)
		}
	}
	if p.ArchivedAt != nil {
		plan.ArchivedAt = timestamppb.New(*p.ArchivedAt)
	}
	if p.Metadata != nil {
		var m map[string]any
		if err := json.Unmarshal([]byte(*p.Metadata), &m); err == nil {
			if s, err := structpb.NewStruct(m); err == nil {
				plan.Metadata = s
			}
		}
	}
	return plan, nil
}

// planEntriesToJSON marshals each entry as protojson and joins into a JSON
// array. We use protojson per-entry rather than json.Marshal so the proto
// field names + oneof discrimination round-trip correctly.
func planEntriesToJSON(entries []*meteryv1.PlanEntry) (string, error) {
	if len(entries) == 0 {
		return "[]", nil
	}
	marshaler := protojson.MarshalOptions{UseProtoNames: true}
	parts := make([][]byte, len(entries))
	for i, e := range entries {
		b, err := marshaler.Marshal(e)
		if err != nil {
			return "", err
		}
		parts[i] = b
	}
	return "[" + string(bytes.Join(parts, []byte(","))) + "]", nil
}

func planEntriesFromJSON(s string) ([]*meteryv1.PlanEntry, error) {
	if s == "" {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]*meteryv1.PlanEntry, len(raw))
	for i, r := range raw {
		e := &meteryv1.PlanEntry{}
		if err := protojson.Unmarshal(r, e); err != nil {
			return nil, err
		}
		out[i] = e
	}
	return out, nil
}
