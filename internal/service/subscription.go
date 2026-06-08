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
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) CreateSubscription(ctx context.Context, req *connect.Request[meteryv1.CreateSubscriptionRequest]) (*connect.Response[meteryv1.CreateSubscriptionResponse], error) {
	c, err := s.store.GetCustomer(ctx, req.Msg.CustomerIdOrKey)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
	}
	p, err := s.store.GetPlan(ctx, req.Msg.PlanIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("plan not found"))
	}
	if p.ArchivedAt != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("plan is archived"))
	}

	// One active subscription per (customer, plan). A duplicate would stack
	// grants on the shared entitlement and double credits every cycle.
	dup, err := s.store.HasActiveSubscriptionForPlan(ctx, c.ID, p.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if dup {
		return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("customer already has an active subscription to this plan"))
	}

	startsAt := time.Now().UTC().Truncate(time.Second)
	if req.Msg.StartsAt != nil {
		startsAt = req.Msg.StartsAt.AsTime()
	}
	var endsAt *time.Time
	if req.Msg.EndsAt != nil {
		t := req.Msg.EndsAt.AsTime()
		endsAt = &t
	}

	var metaStr *string
	if req.Msg.Metadata != nil {
		b, err := json.Marshal(req.Msg.Metadata.AsMap())
		if err == nil {
			s := string(b)
			metaStr = &s
		}
	}

	sub := &store.SubscriptionRow{
		ID:         store.NewULID(),
		CustomerID: c.ID,
		PlanID:     p.ID,
		StartsAt:   startsAt,
		EndsAt:     endsAt,
		Metadata:   metaStr,
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
	}

	entries, err := buildSubscriptionEntries(ctx, s, p, startsAt)
	if err != nil {
		return nil, err
	}

	if err := s.store.CreateSubscriptionWithMaterialize(ctx, sub, entries); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&meteryv1.CreateSubscriptionResponse{
		Subscription: subscriptionRowToProto(sub, c.Key, p.Slug),
	}), nil
}

func (s *Service) GetSubscription(ctx context.Context, req *connect.Request[meteryv1.GetSubscriptionRequest]) (*connect.Response[meteryv1.GetSubscriptionResponse], error) {
	sub, err := s.store.GetSubscription(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("subscription not found"))
	}
	c, _ := s.store.GetCustomer(ctx, sub.CustomerID)
	p, _ := s.store.GetPlan(ctx, sub.PlanID)
	customerKey, planSlug := "", ""
	if c != nil {
		customerKey = c.Key
	}
	if p != nil {
		planSlug = p.Slug
	}
	return connect.NewResponse(&meteryv1.GetSubscriptionResponse{
		Subscription: subscriptionRowToProto(sub, customerKey, planSlug),
	}), nil
}

func (s *Service) ListSubscriptions(ctx context.Context, req *connect.Request[meteryv1.ListSubscriptionsRequest]) (*connect.Response[meteryv1.ListSubscriptionsResponse], error) {
	customerID := ""
	customerKey := ""
	if req.Msg.CustomerIdOrKey != nil && *req.Msg.CustomerIdOrKey != "" {
		c, err := s.store.GetCustomer(ctx, *req.Msg.CustomerIdOrKey)
		if err != nil {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("customer not found"))
		}
		customerID = c.ID
		customerKey = c.Key
	}

	limit := 100
	if req.Msg.Limit != nil {
		limit = int(*req.Msg.Limit)
	}
	after := ""
	if req.Msg.After != nil {
		after = *req.Msg.After
	}

	rows, err := s.store.ListSubscriptions(ctx, customerID, req.Msg.IncludeCanceled, limit, after)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	// Resolve plan slugs (and customer keys when cross-customer listing).
	planSlugs := map[string]string{}
	customerKeys := map[string]string{customerID: customerKey}

	res := make([]*meteryv1.Subscription, len(rows))
	for i := range rows {
		r := &rows[i]
		ckey, ok := customerKeys[r.CustomerID]
		if !ok {
			c, _ := s.store.GetCustomer(ctx, r.CustomerID)
			if c != nil {
				ckey = c.Key
			}
			customerKeys[r.CustomerID] = ckey
		}
		pslug, ok := planSlugs[r.PlanID]
		if !ok {
			p, _ := s.store.GetPlan(ctx, r.PlanID)
			if p != nil {
				pslug = p.Slug
			}
			planSlugs[r.PlanID] = pslug
		}
		res[i] = subscriptionRowToProto(r, ckey, pslug)
	}
	return connect.NewResponse(&meteryv1.ListSubscriptionsResponse{Subscriptions: res}), nil
}

func (s *Service) CancelSubscription(ctx context.Context, req *connect.Request[meteryv1.CancelSubscriptionRequest]) (*connect.Response[meteryv1.CancelSubscriptionResponse], error) {
	at := time.Now().UTC().Truncate(time.Second)
	if req.Msg.At != nil {
		at = req.Msg.At.AsTime()
	}
	if err := s.store.CancelSubscription(ctx, req.Msg.Id, at); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&meteryv1.CancelSubscriptionResponse{}), nil
}

func (s *Service) ChangeSubscription(ctx context.Context, req *connect.Request[meteryv1.ChangeSubscriptionRequest]) (*connect.Response[meteryv1.ChangeSubscriptionResponse], error) {
	sub, err := s.store.GetSubscription(ctx, req.Msg.Id)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("subscription not found"))
	}
	if sub.CanceledAt != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("subscription is cancelled"))
	}
	newPlan, err := s.store.GetPlan(ctx, req.Msg.PlanIdOrSlug)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("plan not found"))
	}
	if newPlan.ArchivedAt != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("plan is archived"))
	}

	effectiveAt := time.Now().UTC().Truncate(time.Second)
	if req.Msg.EffectiveAt != nil {
		effectiveAt = req.Msg.EffectiveAt.AsTime()
	}

	entries, err := buildSubscriptionEntries(ctx, s, newPlan, effectiveAt)
	if err != nil {
		return nil, err
	}

	if err := s.store.ChangeSubscription(ctx, sub, newPlan.ID, effectiveAt, entries); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	updated, err := s.store.GetSubscription(ctx, sub.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	c, _ := s.store.GetCustomer(ctx, updated.CustomerID)
	customerKey := ""
	if c != nil {
		customerKey = c.Key
	}
	return connect.NewResponse(&meteryv1.ChangeSubscriptionResponse{
		Subscription: subscriptionRowToProto(updated, customerKey, newPlan.Slug),
	}), nil
}

// buildSubscriptionEntries decodes the plan's stored entries, resolves feature
// slugs to IDs, parses durations, and returns store-ready entries with absolute
// timestamps. expires_at is computed relative to startsAt.
func buildSubscriptionEntries(ctx context.Context, s *Service, p *store.PlanRow, startsAt time.Time) ([]store.SubscriptionEntry, error) {
	planEntries, err := planEntriesFromJSON(p.Entries)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("plan entries corrupt"))
	}

	out := make([]store.SubscriptionEntry, 0, len(planEntries))
	for _, pe := range planEntries {
		feat, err := s.store.GetFeature(ctx, pe.FeatureSlug)
		if err != nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("feature not found: "+pe.FeatureSlug))
		}

		entry := store.SubscriptionEntry{FeatureID: feat.ID}
		if pe.UsagePeriod != nil {
			d := pe.UsagePeriod.Duration
			entry.UsagePeriodDuration = &d
			if pe.UsagePeriod.Anchor != nil {
				t := pe.UsagePeriod.Anchor.AsTime()
				entry.UsagePeriodAnchor = &t
			}
		}
		if pe.Rollover != nil {
			max := pe.Rollover.MaxAmount
			entry.RolloverMax = &max
		}

		if pe.Grant != nil {
			if feat.MeterID == nil {
				return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("plan entry has grant template for boolean feature: "+pe.FeatureSlug))
			}
			g := pe.Grant
			priority := int32(100)
			if g.Priority != nil {
				priority = *g.Priority
			}
			mg := &store.MaterializeGrant{
				Amount:   g.Amount,
				Priority: priority,
			}
			if g.Expiration != nil {
				dur, err := duration.Parse(g.Expiration.Duration)
				if err != nil {
					return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invalid expiration duration"))
				}
				exp := shiftTime(startsAt, dur)
				mg.ExpiresAt = &exp
			}
			entry.Grant = mg
		}

		out = append(out, entry)
	}

	return out, nil
}

func subscriptionRowToProto(sub *store.SubscriptionRow, customerKey, planSlug string) *meteryv1.Subscription {
	r := &meteryv1.Subscription{
		Id:          sub.ID,
		CustomerKey: customerKey,
		PlanSlug:    planSlug,
		StartsAt:    timestamppb.New(sub.StartsAt),
		CreatedAt:   timestamppb.New(sub.CreatedAt),
	}
	if sub.EndsAt != nil {
		r.EndsAt = timestamppb.New(*sub.EndsAt)
	}
	if sub.CanceledAt != nil {
		r.CanceledAt = timestamppb.New(*sub.CanceledAt)
	}
	if sub.Metadata != nil {
		var m map[string]any
		if err := json.Unmarshal([]byte(*sub.Metadata), &m); err == nil {
			if s, err := structpb.NewStruct(m); err == nil {
				r.Metadata = s
			}
		}
	}
	return r
}
