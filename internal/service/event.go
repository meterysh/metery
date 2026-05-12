package service

import (
	"context"
	"encoding/json"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
)

func (s *Service) IngestEvent(ctx context.Context, req *connect.Request[meteryv1.IngestEventRequest]) (*connect.Response[meteryv1.IngestEventResponse], error) {
	payloadStr := "{}"
	if req.Msg.Payload != nil {
		b, err := json.Marshal(req.Msg.Payload.AsMap())
		if err == nil {
			payloadStr = string(b)
		}
	}

	t := time.Now().UTC().Truncate(time.Second)
	if req.Msg.Time != nil {
		t = req.Msg.Time.AsTime()
	}

	e := &store.Event{
		ID:        req.Msg.Id,
		Customer:  req.Msg.Customer,
		Type:      req.Msg.Type,
		Time:      t,
		Payload:   &payloadStr,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := s.store.IngestEvent(ctx, e); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&meteryv1.IngestEventResponse{}), nil
}
