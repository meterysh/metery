package service

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
)

func TestSubscribe_MaterialisesEntitlementAndGrant(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{
			FeatureSlug: featSlug,
			Grant:       &meteryv1.GrantTemplate{Amount: 500},
		}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	subResp, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey,
		PlanIdOrSlug:    "pro",
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	subID := subResp.Msg.Subscription.Id

	cust, err := st.GetCustomer(ctx, custKey)
	if err != nil {
		t.Fatalf("get customer: %v", err)
	}
	feat, err := st.GetFeature(ctx, featSlug)
	if err != nil {
		t.Fatalf("get feature: %v", err)
	}
	ent, err := st.GetEntitlement(ctx, cust.ID, feat.ID)
	if err != nil {
		t.Fatalf("entitlement not materialised: %v", err)
	}
	if ent.SubscriptionID == nil || *ent.SubscriptionID != subID {
		t.Errorf("entitlement.subscription_id = %v, want %s", ent.SubscriptionID, subID)
	}

	grants, err := st.ListGrants(ctx, ent.ID, false, 100, "")
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("grant count = %d, want 1", len(grants))
	}
	g := grants[0]
	if g.Amount != 500 {
		t.Errorf("grant amount = %d, want 500", g.Amount)
	}
	if g.SubscriptionID == nil || *g.SubscriptionID != subID {
		t.Errorf("grant.subscription_id = %v, want %s", g.SubscriptionID, subID)
	}
}

func TestSubscribe_ReusesExistingEntitlement(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.entitlement.CreateEntitlement(ctx, connect.NewRequest(&meteryv1.CreateEntitlementRequest{
		CustomerIdOrKey: custKey,
		FeatureIdOrSlug: featSlug,
	})); err != nil {
		t.Fatalf("create manual entitlement: %v", err)
	}

	cust, _ := st.GetCustomer(ctx, custKey)
	feat, _ := st.GetFeature(ctx, featSlug)
	manualEnt, _ := st.GetEntitlement(ctx, cust.ID, feat.ID)

	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{
			FeatureSlug: featSlug,
			Grant:       &meteryv1.GrantTemplate{Amount: 500},
		}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	subResp, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "pro",
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Same entitlement row, still NULL subscription_id (stayed manual).
	ent, _ := st.GetEntitlement(ctx, cust.ID, feat.ID)
	if ent.ID != manualEnt.ID {
		t.Errorf("entitlement id changed: was %s, now %s", manualEnt.ID, ent.ID)
	}
	if ent.SubscriptionID != nil {
		t.Errorf("manual entitlement should stay manual; subscription_id = %v", *ent.SubscriptionID)
	}

	// Grant created and tagged to subscription.
	grants, _ := st.ListGrants(ctx, ent.ID, false, 100, "")
	if len(grants) != 1 {
		t.Fatalf("grant count = %d, want 1", len(grants))
	}
	if grants[0].SubscriptionID == nil || *grants[0].SubscriptionID != subResp.Msg.Subscription.Id {
		t.Errorf("grant.subscription_id = %v, want %s", grants[0].SubscriptionID, subResp.Msg.Subscription.Id)
	}
}

func TestCancelSubscription_VoidsOnlyOwnedGrants(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{
			FeatureSlug: featSlug,
			Grant:       &meteryv1.GrantTemplate{Amount: 500},
		}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	subResp, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "pro",
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	if _, err := c.grant.CreateGrant(ctx, connect.NewRequest(&meteryv1.CreateGrantRequest{
		CustomerIdOrKey: custKey,
		FeatureIdOrSlug: featSlug,
		Amount:          100,
		Metadata:        nil,
	})); err != nil {
		t.Fatalf("create manual grant: %v", err)
	}

	if _, err := c.subscription.CancelSubscription(ctx, connect.NewRequest(&meteryv1.CancelSubscriptionRequest{
		Id: subResp.Msg.Subscription.Id,
	})); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	cust, _ := st.GetCustomer(ctx, custKey)
	feat, _ := st.GetFeature(ctx, featSlug)
	ent, _ := st.GetEntitlement(ctx, cust.ID, feat.ID)
	allGrants, _ := st.ListGrants(ctx, ent.ID, true, 100, "")
	var subOwnedVoided, manualVoided int
	for _, g := range allGrants {
		if g.SubscriptionID != nil {
			if g.VoidedAt != nil {
				subOwnedVoided++
			}
		} else {
			if g.VoidedAt != nil {
				manualVoided++
			}
		}
	}
	if subOwnedVoided != 1 {
		t.Errorf("expected 1 sub-owned grant voided, got %d", subOwnedVoided)
	}
	if manualVoided != 0 {
		t.Errorf("manual grant should not be voided, got %d voided", manualVoided)
	}

	sub, _ := st.GetSubscription(ctx, subResp.Msg.Subscription.Id)
	if sub.CanceledAt == nil {
		t.Errorf("sub.canceled_at not set")
	}
	if sub.EndsAt == nil {
		t.Errorf("sub.ends_at should default to canceled_at when unset")
	}
}

func TestChangeSubscription_SwapsGrants(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	for _, p := range []struct {
		slug string
		amt  int64
	}{{"basic", 100}, {"pro", 1000}} {
		if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
			Slug: p.slug, Name: p.slug,
			Entries: []*meteryv1.PlanEntry{{FeatureSlug: featSlug, Grant: &meteryv1.GrantTemplate{Amount: p.amt}}},
		})); err != nil {
			t.Fatalf("create plan %s: %v", p.slug, err)
		}
	}

	subResp, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "basic",
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	changeResp, err := c.subscription.ChangeSubscription(ctx, connect.NewRequest(&meteryv1.ChangeSubscriptionRequest{
		Id: subResp.Msg.Subscription.Id, PlanIdOrSlug: "pro",
	}))
	if err != nil {
		t.Fatalf("change: %v", err)
	}
	if changeResp.Msg.Subscription.PlanSlug != "pro" {
		t.Errorf("plan_slug = %q, want pro", changeResp.Msg.Subscription.PlanSlug)
	}

	cust, _ := st.GetCustomer(ctx, custKey)
	feat, _ := st.GetFeature(ctx, featSlug)
	ent, _ := st.GetEntitlement(ctx, cust.ID, feat.ID)
	allGrants, _ := st.ListGrants(ctx, ent.ID, true, 100, "")
	var voidedBasic, activePro int
	for _, g := range allGrants {
		switch {
		case g.Amount == 100 && g.VoidedAt != nil:
			voidedBasic++
		case g.Amount == 1000 && g.VoidedAt == nil:
			activePro++
		}
	}
	if voidedBasic != 1 {
		t.Errorf("expected 1 voided basic grant, got %d", voidedBasic)
	}
	if activePro != 1 {
		t.Errorf("expected 1 active pro grant, got %d", activePro)
	}
}

func TestSubscribe_ArchivedPlanRejected(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "old", Name: "Old",
		Entries: []*meteryv1.PlanEntry{{FeatureSlug: featSlug, Grant: &meteryv1.GrantTemplate{Amount: 100}}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if _, err := c.plan.ArchivePlan(ctx, connect.NewRequest(&meteryv1.ArchivePlanRequest{IdOrSlug: "old"})); err != nil {
		t.Fatalf("archive plan: %v", err)
	}

	_, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "old",
	}))
	if err == nil {
		t.Fatalf("expected error subscribing to archived plan")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Errorf("err code = %v, want FailedPrecondition", got)
	}
}

func TestSubscribe_BooleanFeatureNoGrant(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()

	if _, err := c.customer.CreateCustomer(ctx, connect.NewRequest(&meteryv1.CreateCustomerRequest{Key: "u1", Name: "u1"})); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if _, err := c.feature.CreateFeature(ctx, connect.NewRequest(&meteryv1.CreateFeatureRequest{
		Slug: "early_access", Name: "Early Access",
	})); err != nil {
		t.Fatalf("create boolean feature: %v", err)
	}
	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "beta", Name: "Beta",
		Entries: []*meteryv1.PlanEntry{{FeatureSlug: "early_access"}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	subResp, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: "u1", PlanIdOrSlug: "beta",
	}))
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	cust, _ := st.GetCustomer(ctx, "u1")
	feat, _ := st.GetFeature(ctx, "early_access")
	ent, err := st.GetEntitlement(ctx, cust.ID, feat.ID)
	if err != nil {
		t.Fatalf("entitlement not materialised for boolean: %v", err)
	}
	if ent.SubscriptionID == nil || *ent.SubscriptionID != subResp.Msg.Subscription.Id {
		t.Errorf("ent.subscription_id = %v, want %s", ent.SubscriptionID, subResp.Msg.Subscription.Id)
	}
	grants, _ := st.ListGrants(ctx, ent.ID, true, 100, "")
	if len(grants) != 0 {
		t.Errorf("expected no grants for boolean entry, got %d", len(grants))
	}
}

func TestSubscribe_BooleanFeatureWithGrantRejected(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()

	if _, err := c.customer.CreateCustomer(ctx, connect.NewRequest(&meteryv1.CreateCustomerRequest{Key: "u1", Name: "u1"})); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if _, err := c.feature.CreateFeature(ctx, connect.NewRequest(&meteryv1.CreateFeatureRequest{
		Slug: "early_access", Name: "Early Access",
	})); err != nil {
		t.Fatalf("create boolean feature: %v", err)
	}
	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "bad", Name: "Bad",
		Entries: []*meteryv1.PlanEntry{{
			FeatureSlug: "early_access",
			Grant:       &meteryv1.GrantTemplate{Amount: 100},
		}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	_, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: "u1", PlanIdOrSlug: "bad",
	}))
	if err == nil {
		t.Fatalf("expected error subscribing boolean feature with grant template")
	}
	if got := connect.CodeOf(err); got != connect.CodeFailedPrecondition {
		t.Errorf("err code = %v, want FailedPrecondition", got)
	}
}

func TestListSubscriptions_FilterByCustomer(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	_, featSlug := seedDeps(t, ctx, c)

	if _, err := c.customer.CreateCustomer(ctx, connect.NewRequest(&meteryv1.CreateCustomerRequest{Key: "u2", Name: "u2"})); err != nil {
		t.Fatalf("create customer u2: %v", err)
	}
	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{FeatureSlug: featSlug, Grant: &meteryv1.GrantTemplate{Amount: 100}}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	for _, k := range []string{"u1", "u2"} {
		if _, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
			CustomerIdOrKey: k, PlanIdOrSlug: "pro",
		})); err != nil {
			t.Fatalf("subscribe %s: %v", k, err)
		}
	}

	all, err := c.subscription.ListSubscriptions(ctx, connect.NewRequest(&meteryv1.ListSubscriptionsRequest{}))
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all.Msg.Subscriptions) != 2 {
		t.Errorf("top-level list = %d subs, want 2", len(all.Msg.Subscriptions))
	}

	one, err := c.subscription.ListSubscriptions(ctx, connect.NewRequest(&meteryv1.ListSubscriptionsRequest{
		CustomerIdOrKey: ptr("u1"),
	}))
	if err != nil {
		t.Fatalf("list u1: %v", err)
	}
	if len(one.Msg.Subscriptions) != 1 {
		t.Errorf("customer-filtered list = %d subs, want 1", len(one.Msg.Subscriptions))
	}
	if one.Msg.Subscriptions[0].CustomerKey != "u1" {
		t.Errorf("customer_key = %q, want u1", one.Msg.Subscriptions[0].CustomerKey)
	}
}

// Manual grants/entitlements still work after subscription_id column added.
func TestManualGrantStillWorks_AfterSchemaChange(t *testing.T) {
	st, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.entitlement.CreateEntitlement(ctx, connect.NewRequest(&meteryv1.CreateEntitlementRequest{
		CustomerIdOrKey: custKey, FeatureIdOrSlug: featSlug,
	})); err != nil {
		t.Fatalf("create entitlement: %v", err)
	}
	if _, err := c.grant.CreateGrant(ctx, connect.NewRequest(&meteryv1.CreateGrantRequest{
		CustomerIdOrKey: custKey, FeatureIdOrSlug: featSlug, Amount: 42,
	})); err != nil {
		t.Fatalf("create grant: %v", err)
	}

	cust, _ := st.GetCustomer(ctx, custKey)
	feat, _ := st.GetFeature(ctx, featSlug)
	ent, _ := st.GetEntitlement(ctx, cust.ID, feat.ID)
	if ent.SubscriptionID != nil {
		t.Errorf("manual entitlement.subscription_id should be nil, got %v", *ent.SubscriptionID)
	}
	grants, _ := st.ListGrants(ctx, ent.ID, false, 100, "")
	if len(grants) != 1 {
		t.Fatalf("grant count = %d, want 1", len(grants))
	}
	if grants[0].SubscriptionID != nil {
		t.Errorf("manual grant.subscription_id should be nil, got %v", *grants[0].SubscriptionID)
	}
}

// A customer may hold at most one active subscription per plan; a second
// subscribe to the same plan is rejected. Re-subscribing after cancel is OK.
func TestSubscribe_DuplicateActivePlanRejected(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()
	c := newTestClients(ts)
	ctx := context.Background()
	custKey, featSlug := seedDeps(t, ctx, c)

	if _, err := c.plan.CreatePlan(ctx, connect.NewRequest(&meteryv1.CreatePlanRequest{
		Slug: "pro", Name: "Pro",
		Entries: []*meteryv1.PlanEntry{{FeatureSlug: featSlug, Grant: &meteryv1.GrantTemplate{Amount: 500}}},
	})); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	sub, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "pro",
	}))
	if err != nil {
		t.Fatalf("first subscribe: %v", err)
	}

	_, err = c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "pro",
	}))
	if err == nil {
		t.Fatal("expected duplicate active subscription to be rejected")
	}
	if got := connect.CodeOf(err); got != connect.CodeAlreadyExists {
		t.Errorf("err code = %v, want AlreadyExists", got)
	}

	// After cancel, re-subscribing is allowed.
	if _, err := c.subscription.CancelSubscription(ctx, connect.NewRequest(&meteryv1.CancelSubscriptionRequest{
		Id: sub.Msg.Subscription.Id,
	})); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := c.subscription.CreateSubscription(ctx, connect.NewRequest(&meteryv1.CreateSubscriptionRequest{
		CustomerIdOrKey: custKey, PlanIdOrSlug: "pro",
	})); err != nil {
		t.Fatalf("re-subscribe after cancel: %v", err)
	}
}
