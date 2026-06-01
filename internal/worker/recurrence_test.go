package worker

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/meterysh/metery/internal/store"
	"github.com/meterysh/metery/internal/store/migrations"
	"github.com/pressly/goose/v3"
)

// TestProcessRecurrence_EmitsNextCycleGrant exercises the new
// subscription-walking worker: a recurring plan with a single metered
// entry, backdated subscription, one initial grant — the worker should
// emit exactly one new grant linked to the subscription.
func TestProcessRecurrence_EmitsNextCycleGrant(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	now := time.Now().UTC().Truncate(time.Second)
	startsAt := now.Add(-32 * 24 * time.Hour) // ~one cycle ago

	// Customer + meter + metered feature.
	customer := &store.Customer{ID: store.NewULID(), Key: "user_w", Name: "Worker Test", CreatedAt: now}
	if err := st.CreateCustomer(ctx, customer); err != nil {
		t.Fatal(err)
	}
	meter := &store.Meter{ID: store.NewULID(), Slug: "credits_w", Name: "Credits", Aggregation: "sum", EventType: "credit_spend", CreatedAt: now}
	if err := st.CreateMeter(ctx, meter); err != nil {
		t.Fatal(err)
	}
	feature := &store.Feature{ID: store.NewULID(), Slug: "credits_w", Name: "Credits", MeterID: &meter.ID, CreatedAt: now}
	if err := st.CreateFeature(ctx, feature); err != nil {
		t.Fatal(err)
	}

	// Plan with monthly recurrence + one entry that grants 100 credits.
	interval := "P1M"
	plan := &store.PlanRow{
		ID:                 store.NewULID(),
		Slug:               "monthly_w",
		Name:               "Monthly",
		Entries:            `[{"feature_slug":"credits_w","grant":{"amount":"100","priority":50}}]`,
		RecurrenceInterval: &interval,
		CreatedAt:          now,
	}
	if err := st.CreatePlan(ctx, plan); err != nil {
		t.Fatal(err)
	}

	// Subscribe + materialise the initial grant. Done by hand here so
	// we control starts_at; the service path would do this too.
	sub := &store.SubscriptionRow{
		ID:         store.NewULID(),
		CustomerID: customer.ID,
		PlanID:     plan.ID,
		StartsAt:   startsAt,
		CreatedAt:  startsAt,
	}
	entries := []store.SubscriptionEntry{{
		FeatureID: feature.ID,
		Grant:     &store.MaterializeGrant{Amount: 100, Priority: 50},
	}}
	if err := st.CreateSubscriptionWithMaterialize(ctx, sub, entries); err != nil {
		t.Fatal(err)
	}

	// Before the worker runs: exactly one grant on the subscription.
	custEnt, _ := st.GetEntitlement(ctx, customer.ID, feature.ID)
	before, _ := st.ListGrants(ctx, custEnt.ID, true, 100, "")
	if len(before) != 1 {
		t.Fatalf("expected 1 initial grant before worker, got %d", len(before))
	}

	// Drive one worker pass.
	processRecurrence(ctx, st)

	after, _ := st.ListGrants(ctx, custEnt.ID, true, 100, "")
	if len(after) != 2 {
		t.Fatalf("expected 2 grants after worker pass (1 initial + 1 emitted), got %d", len(after))
	}

	var initial, emitted *store.GrantRow
	for i := range after {
		g := &after[i]
		if g.EffectiveAt.Equal(startsAt) {
			initial = g
		} else {
			emitted = g
		}
	}
	if initial == nil || emitted == nil {
		t.Fatalf("could not identify initial vs emitted grant: %+v", after)
	}
	if emitted.SubscriptionID == nil || *emitted.SubscriptionID != sub.ID {
		t.Errorf("emitted grant subscription_id = %v, want %s", emitted.SubscriptionID, sub.ID)
	}
	if emitted.Amount != 100 || emitted.Priority != 50 {
		t.Errorf("emitted grant amount/priority = %d/%d, want 100/50", emitted.Amount, emitted.Priority)
	}
	expectEff := startsAt.AddDate(0, 1, 0)
	if !emitted.EffectiveAt.Equal(expectEff) {
		t.Errorf("emitted grant effective_at = %s, want %s", emitted.EffectiveAt, expectEff)
	}

	// Second pass should be a no-op (UNIQUE constraint + nextTime > now
	// after the first emission). Total still 2.
	processRecurrence(ctx, st)
	again, _ := st.ListGrants(ctx, custEnt.ID, true, 100, "")
	if len(again) != 2 {
		t.Errorf("second pass should be idempotent, got %d grants", len(again))
	}
}

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dbName := "test_" + store.NewULID()
	db, err := sql.Open("sqlite", "file:"+dbName+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return store.New(db, "sqlite")
}
