package service

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// IngestEvent must be idempotent on `id` — replays return success and do not
// double-count usage. v0 implementation relies on ON CONFLICT(id) DO NOTHING;
// regressions are most likely to come from migrations that drop the PK
// uniqueness or service code that strips it.
func TestIngestEvent_IsIdempotentOnID(t *testing.T) {
	_, ts := setupTestServer(t)
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
		CustomerIdOrKey: custKey, FeatureIdOrSlug: featSlug, Amount: 1000,
	})); err != nil {
		t.Fatalf("create grant: %v", err)
	}

	eventTime := timestamppb.New(time.Now().UTC().Add(time.Second).Truncate(time.Second))
	payload1, _ := structpb.NewStruct(map[string]any{"amount": float64(50)})
	payload2, _ := structpb.NewStruct(map[string]any{"amount": float64(999)}) // would change usage if dedup failed

	for i := 0; i < 2; i++ {
		payload := payload1
		if i == 1 {
			payload = payload2
		}
		_, err := c.event.IngestEvent(ctx, connect.NewRequest(&meteryv1.IngestEventRequest{
			Id:       "evt_dedup",
			Customer: custKey,
			Type:     "credit_spend",
			Time:     eventTime,
			Payload:  payload,
		}))
		if err != nil {
			t.Fatalf("ingest #%d (must succeed silently on replay): %v", i+1, err)
		}
	}

	// Evaluate strictly after the event time so the period includes it.
	evalAt := timestamppb.New(eventTime.AsTime().Add(time.Second))
	val, err := c.entitlement.GetEntitlementValue(ctx, connect.NewRequest(&meteryv1.GetEntitlementValueRequest{
		CustomerIdOrKey: custKey, FeatureIdOrSlug: featSlug, At: evalAt,
	}))
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if val.Msg.Value.Usage == nil {
		t.Fatalf("usage not populated")
	}
	if got := *val.Msg.Value.Usage; got != 50 {
		t.Errorf("usage = %d, want 50 (replay was deduped, second payload of 999 must NOT count)", got)
	}
}
