package service

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) CreateCustomer(ctx context.Context, req *connect.Request[meteryv1.CreateCustomerRequest]) (*connect.Response[meteryv1.CreateCustomerResponse], error) {
	c := &store.Customer{
		ID:        store.NewULID(),
		Key:       req.Msg.Key,
		Name:      req.Msg.Name,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := s.store.CreateCustomer(ctx, c); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.CreateCustomerResponse{
		Customer: &meteryv1.Customer{
			Id:        c.ID,
			Key:       c.Key,
			Name:      c.Name,
			CreatedAt: timestamppb.New(c.CreatedAt),
		},
	}), nil
}

func (s *Service) GetCustomer(ctx context.Context, req *connect.Request[meteryv1.GetCustomerRequest]) (*connect.Response[meteryv1.GetCustomerResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.IdOrKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.GetCustomerResponse{
		Customer: &meteryv1.Customer{
			Id:   c.ID,
			Key:  c.Key,
			Name: c.Name,
		},
	}), nil
}

func (s *Service) ListCustomers(ctx context.Context, req *connect.Request[meteryv1.ListCustomersRequest]) (*connect.Response[meteryv1.ListCustomersResponse], error) {
	limit := 100
	if req.Msg.Limit != nil && *req.Msg.Limit > 0 {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}
	cs, err := s.store.ListCustomers(ctx, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := make([]*meteryv1.Customer, 0, len(cs))
	for _, c := range cs {
		if !req.Msg.IncludeDeactivated && c.DeactivatedAt != nil {
			continue
		}
		res = append(res, customerToProto(&c))
	}
	return connect.NewResponse(&meteryv1.ListCustomersResponse{Customers: res}), nil
}

func (s *Service) UpdateCustomer(ctx context.Context, req *connect.Request[meteryv1.UpdateCustomerRequest]) (*connect.Response[meteryv1.UpdateCustomerResponse], error) {
	c, err := s.store.UpdateCustomer(ctx, req.Msg.IdOrKey, req.Msg.Name)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	return connect.NewResponse(&meteryv1.UpdateCustomerResponse{Customer: customerToProto(c)}), nil
}

func (s *Service) DeactivateCustomer(ctx context.Context, req *connect.Request[meteryv1.DeactivateCustomerRequest]) (*connect.Response[meteryv1.DeactivateCustomerResponse], error) {
	if err := s.store.DeactivateCustomer(ctx, req.Msg.IdOrKey); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.DeactivateCustomerResponse{}), nil
}

func customerToProto(c *store.Customer) *meteryv1.Customer {
	r := &meteryv1.Customer{
		Id:        c.ID,
		Key:       c.Key,
		Name:      c.Name,
		CreatedAt: timestamppb.New(c.CreatedAt),
	}
	if c.DeactivatedAt != nil {
		r.DeactivatedAt = timestamppb.New(*c.DeactivatedAt)
	}
	return r
}
