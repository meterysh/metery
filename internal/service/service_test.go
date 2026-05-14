package service

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"connectrpc.com/vanguard"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	_ "modernc.org/sqlite"

	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/gen/go/metery/v1/meteryv1connect"
	"github.com/meterysh/metery/internal/auth"
	"google.golang.org/protobuf/types/known/timestamppb"
	"github.com/meterysh/metery/internal/store"
	"github.com/meterysh/metery/internal/store/migrations"
	"github.com/pressly/goose/v3"
)

func setupTestServer(t *testing.T) (*store.Store, *httptest.Server) {
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("failed to set dialect: %v", err)
	}
	if err := goose.Up(db, "sqlite"); err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}

	st := store.New(db, "sqlite")
	srv := NewService(st)

	apiKeys := []string{"mtr_testkey"}
	opts := connect.WithInterceptors(auth.AuthMiddleware(apiKeys))

	services := []*vanguard.Service{
		vanguard.NewService(meteryv1connect.NewCustomerServiceHandler(srv, opts)),
		vanguard.NewService(meteryv1connect.NewMeterServiceHandler(srv, opts)),
		vanguard.NewService(meteryv1connect.NewFeatureServiceHandler(srv, opts)),
		vanguard.NewService(meteryv1connect.NewEntitlementServiceHandler(srv, opts)),
		vanguard.NewService(meteryv1connect.NewGrantServiceHandler(srv, opts)),
		vanguard.NewService(meteryv1connect.NewEventServiceHandler(srv, opts)),
	}

	transcoder, err := vanguard.NewTranscoder(services,
		vanguard.WithCodec(func(res vanguard.TypeResolver) vanguard.Codec {
			codec := vanguard.NewJSONCodec(res)
			codec.MarshalOptions.UseProtoNames = true
					codec.MarshalOptions.EmitUnpopulated = true
			codec.UnmarshalOptions.DiscardUnknown = true
			return codec
		}),
	)
	if err != nil {
		t.Fatalf("failed to init vanguard: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", transcoder)

	h2cSrv := &http2.Server{}
	handler := h2c.NewHandler(mux, h2cSrv)

	ts := httptest.NewServer(handler)
	return st, ts
}

func TestEndToEndV0HappyPath(t *testing.T) {
	_, ts := setupTestServer(t)
	defer ts.Close()

	httpClient := ts.Client()

	authOpt := connect.WithInterceptors(connect.UnaryInterceptorFunc(func(next connect.UnaryFunc) connect.UnaryFunc {
		return connect.UnaryFunc(func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			req.Header().Set("Authorization", "Bearer mtr_testkey")
			return next(ctx, req)
		})
	}))

	custClient := meteryv1connect.NewCustomerServiceClient(httpClient, ts.URL, authOpt)
	meterClient := meteryv1connect.NewMeterServiceClient(httpClient, ts.URL, authOpt)
	featClient := meteryv1connect.NewFeatureServiceClient(httpClient, ts.URL, authOpt)
	entClient := meteryv1connect.NewEntitlementServiceClient(httpClient, ts.URL, authOpt)
	grantClient := meteryv1connect.NewGrantServiceClient(httpClient, ts.URL, authOpt)
	eventClient := meteryv1connect.NewEventServiceClient(httpClient, ts.URL, authOpt)

	ctx := context.Background()

	// 1. Create Customer
	cResp, err := custClient.CreateCustomer(ctx, connect.NewRequest(&meteryv1.CreateCustomerRequest{
		Key:  "user_123",
		Name: "Acme Corp",
	}))
	if err != nil {
		t.Fatalf("create customer failed: %v", err)
	}

	// 2. Create Meter
	mResp, err := meterClient.CreateMeter(ctx, connect.NewRequest(&meteryv1.CreateMeterRequest{
		Slug:        "api_calls",
		Name:        "API Calls",
		Aggregation: "count",
		EventType:   "api_call",
	}))
	if err != nil {
		t.Fatalf("create meter failed: %v", err)
	}

	// 3. Create Feature
	fResp, err := featClient.CreateFeature(ctx, connect.NewRequest(&meteryv1.CreateFeatureRequest{
		Slug:      "api_calls_feature",
		Name:      "API Calls",
		MeterSlug: mResp.Msg.Meter.Slug,
	}))
	if err != nil {
		t.Fatalf("create feature failed: %v", err)
	}

	// 4. Create Entitlement
	_, err = entClient.CreateEntitlement(ctx, connect.NewRequest(&meteryv1.CreateEntitlementRequest{
		CustomerIdOrKey: cResp.Msg.Customer.Key,
		FeatureIdOrSlug: fResp.Msg.Feature.Slug,
	}))
	if err != nil {
		t.Fatalf("create entitlement failed: %v", err)
	}

	// 5. Create Grant
	_, err = grantClient.CreateGrant(ctx, connect.NewRequest(&meteryv1.CreateGrantRequest{
		CustomerIdOrKey: cResp.Msg.Customer.Key,
		FeatureIdOrSlug: fResp.Msg.Feature.Slug,
		Amount:          1000,
	}))
	if err != nil {
		t.Fatalf("create grant failed: %v", err)
	}

	// 6. Check balance before any usage
	evalAt := timestamppb.Now()
	valResp, err := entClient.GetEntitlementValue(ctx, connect.NewRequest(&meteryv1.GetEntitlementValueRequest{
		CustomerIdOrKey: cResp.Msg.Customer.Key,
		FeatureIdOrSlug: fResp.Msg.Feature.Slug,
		At:              evalAt,
	}))
	if err != nil {
		t.Fatalf("get value failed: %v", err)
	}
	if !valResp.Msg.Value.HasAccess {
		t.Errorf("expected access to be true")
	}
	if *valResp.Msg.Value.Balance != 1000 {
		t.Errorf("expected balance 1000 before usage, got %v", *valResp.Msg.Value.Balance)
	}
	if *valResp.Msg.Value.Usage != 0 {
		t.Errorf("expected usage 0 before any events, got %v", *valResp.Msg.Value.Usage)
	}

	// 7. Ingest event with explicit time anchored to evalAt
	eventTime := timestamppb.New(evalAt.AsTime().Add(time.Second))
	_, err = eventClient.IngestEvent(ctx, connect.NewRequest(&meteryv1.IngestEventRequest{
		Id:       "evt_1",
		Customer: cResp.Msg.Customer.Key,
		Type:     "api_call",
		Time:     eventTime,
	}))
	if err != nil {
		t.Fatalf("ingest event failed: %v", err)
	}

	// 8. Check balance after usage — evaluate after the event time
	evalAt2 := timestamppb.New(eventTime.AsTime().Add(time.Second))
	valResp2, err := entClient.GetEntitlementValue(ctx, connect.NewRequest(&meteryv1.GetEntitlementValueRequest{
		CustomerIdOrKey: cResp.Msg.Customer.Key,
		FeatureIdOrSlug: fResp.Msg.Feature.Slug,
		At:              evalAt2,
	}))
	if err != nil {
		t.Fatalf("get value after event failed: %v", err)
	}
	if !valResp2.Msg.Value.HasAccess {
		t.Errorf("expected access to still be true")
	}
	if *valResp2.Msg.Value.Balance != 999 {
		t.Errorf("expected balance 999 after 1 event, got %v", *valResp2.Msg.Value.Balance)
	}
	if *valResp2.Msg.Value.Usage != 1 {
		t.Errorf("expected usage 1 after 1 event, got %v", *valResp2.Msg.Value.Usage)
	}
}
