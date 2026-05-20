package service

import (
	"context"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/gen/go/metery/v1/meteryv1connect"
)

type testClients struct {
	customer     meteryv1connect.CustomerServiceClient
	meter        meteryv1connect.MeterServiceClient
	feature      meteryv1connect.FeatureServiceClient
	entitlement  meteryv1connect.EntitlementServiceClient
	grant        meteryv1connect.GrantServiceClient
	event        meteryv1connect.EventServiceClient
	plan         meteryv1connect.PlanServiceClient
	subscription meteryv1connect.SubscriptionServiceClient
}

func newTestClients(ts *httptest.Server) testClients {
	authOpt := connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer mtr_testkey")
			return next(ctx, req)
		}
	}))
	return testClients{
		customer:     meteryv1connect.NewCustomerServiceClient(ts.Client(), ts.URL, authOpt),
		meter:        meteryv1connect.NewMeterServiceClient(ts.Client(), ts.URL, authOpt),
		feature:      meteryv1connect.NewFeatureServiceClient(ts.Client(), ts.URL, authOpt),
		entitlement:  meteryv1connect.NewEntitlementServiceClient(ts.Client(), ts.URL, authOpt),
		grant:        meteryv1connect.NewGrantServiceClient(ts.Client(), ts.URL, authOpt),
		event:        meteryv1connect.NewEventServiceClient(ts.Client(), ts.URL, authOpt),
		plan:         meteryv1connect.NewPlanServiceClient(ts.Client(), ts.URL, authOpt),
		subscription: meteryv1connect.NewSubscriptionServiceClient(ts.Client(), ts.URL, authOpt),
	}
}

// seedDeps creates the customer + meter + metered feature most tests need.
// Returns customer key and feature slug.
func seedDeps(t *testing.T, ctx context.Context, c testClients) (custKey, featSlug string) {
	t.Helper()
	if _, err := c.customer.CreateCustomer(ctx, connect.NewRequest(&meteryv1.CreateCustomerRequest{Key: "u1", Name: "User 1"})); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	if _, err := c.meter.CreateMeter(ctx, connect.NewRequest(&meteryv1.CreateMeterRequest{
		Slug: "credits", Name: "Credits", Aggregation: "sum", EventType: "credit_spend", ValueProperty: "amount",
	})); err != nil {
		t.Fatalf("create meter: %v", err)
	}
	if _, err := c.feature.CreateFeature(ctx, connect.NewRequest(&meteryv1.CreateFeatureRequest{
		Slug: "credits", Name: "Credits", MeterSlug: "credits",
	})); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	return "u1", "credits"
}

func ptr[T any](v T) *T { return &v }
