package service

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
)

func TestCreatePlan_RoundTrip(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	seedDeps(t, ctx, c)

	priority := int32(50)
	resp, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{
			FeatureSlug: "credits",
			Grant: &meteryv1.GrantTemplate{
				Amount:     1000,
				Priority:   &priority,
				Recurrence: &meteryv1.Recurrence{Interval: "P1M"},
				Expiration: &meteryv1.Expiration{Duration: "P1M"},
				Rollover:   &meteryv1.Rollover{MaxAmount: 500, Type: "remaining"},
			},
		}},
	}))
	if err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if got, want := resp.Msg.Plan.Slug, "pro"; got != want {
		t.Errorf("slug = %q, want %q", got, want)
	}

	got, err := c.plan.GetPlan(ctx, connect.NewRequest(&meteryv1.GetPlanRequest{IdOrSlug: "pro"}))
	if err != nil {
		t.Fatalf("get plan: %v", err)
	}
	if len(got.Msg.Plan.Entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(got.Msg.Plan.Entries))
	}
	e := got.Msg.Plan.Entries[0]
	if e.FeatureSlug != "credits" {
		t.Errorf("entry feature_slug = %q, want credits", e.FeatureSlug)
	}
	if e.Grant.Amount != 1000 {
		t.Errorf("grant amount = %d, want 1000", e.Grant.Amount)
	}
	if e.Grant.Recurrence == nil || e.Grant.Recurrence.Interval != "P1M" {
		t.Errorf("recurrence not preserved: %+v", e.Grant.Recurrence)
	}
	if e.Grant.Expiration == nil || e.Grant.Expiration.Duration != "P1M" {
		t.Errorf("expiration not preserved: %+v", e.Grant.Expiration)
	}
	if e.Grant.Rollover == nil || e.Grant.Rollover.MaxAmount != 500 || e.Grant.Rollover.Type != "remaining" {
		t.Errorf("rollover not preserved: %+v", e.Grant.Rollover)
	}
}

func TestCreatePlan_UnknownFeatureRejected(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()

	_, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{FeatureSlug: "no_such_feature"}},
	}))
	if err == nil {
		t.Fatalf("expected error for unknown feature, got nil")
	}
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Errorf("err code = %v, want InvalidArgument", got)
	}
}
