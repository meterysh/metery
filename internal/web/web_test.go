package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/meterysh/metery/internal/auth"
	"github.com/meterysh/metery/internal/store"
	"github.com/meterysh/metery/internal/store/migrations"
	"github.com/pressly/goose/v3"
)

func setupDashboard(t *testing.T) (*Handler, *auth.SessionManager, *store.Store, string) {
	t.Helper()
	dbName := "webtest_" + store.NewULID()
	db, err := sql.Open("sqlite", "file:"+dbName+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("sqlite"); err != nil {
		t.Fatalf("dialect: %v", err)
	}
	if err := goose.Up(db, "sqlite"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	st := store.New(db, "sqlite")
	sessions := auth.NewSessionManager([]byte("dashboard-test-secret-0123456789"))
	h := NewHandler(st, sessions)

	u, err := st.UpsertUser(context.Background(), "g-1", "admin@example.com", "Admin")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return h, sessions, st, u.ID
}

// authedGet forges a signed session cookie for userID and runs the handler.
func authedGet(t *testing.T, h http.HandlerFunc, sessions *auth.SessionManager, userID, path string) string {
	t.Helper()
	cookieRec := httptest.NewRecorder()
	sessions.Set(cookieRec, userID)
	cookies := cookieRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("session.Set produced no cookie")
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d", path, w.Code)
	}
	return w.Body.String()
}

func TestDashboard_PlansPageRenders(t *testing.T) {
	h, sessions, st, userID := setupDashboard(t)
	ctx := context.Background()

	if err := st.CreatePlan(ctx, &store.PlanRow{
		ID:        store.NewULID(),
		Slug:      "pro",
		Name:      "Pro Plan",
		Entries:   `[{"feature_slug":"credits"},{"feature_slug":"seats"}]`,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	body := authedGet(t, h.PlansPage, sessions, userID, "/plans")
	for _, want := range []string{"Plans", "pro", "Pro Plan", "credits", "seats"} {
		if !strings.Contains(body, want) {
			t.Errorf("plans page missing %q", want)
		}
	}
}

func TestDashboard_SubscriptionsPageRendersWithStatus(t *testing.T) {
	h, sessions, st, userID := setupDashboard(t)
	ctx := context.Background()

	cust := &store.Customer{ID: store.NewULID(), Key: "alice", Name: "Alice", CreatedAt: time.Now().UTC()}
	if err := st.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	plan := &store.PlanRow{ID: store.NewULID(), Slug: "pro", Name: "Pro", Entries: "[]", CreatedAt: time.Now().UTC()}
	if err := st.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub := &store.SubscriptionRow{
		ID:         store.NewULID(),
		CustomerID: cust.ID,
		PlanID:     plan.ID,
		StartsAt:   time.Now().UTC().Add(-time.Hour),
		CreatedAt:  time.Now().UTC(),
	}
	if err := st.CreateSubscriptionWithMaterialize(ctx, sub, nil); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	body := authedGet(t, h.SubscriptionsPage, sessions, userID, "/subscriptions")
	for _, want := range []string{"Subscriptions", "alice", "pro", "active"} {
		if !strings.Contains(body, want) {
			t.Errorf("subscriptions page missing %q", want)
		}
	}
}

func TestDashboard_OverviewIncludesPlansAndSubscriptions(t *testing.T) {
	h, sessions, st, userID := setupDashboard(t)
	ctx := context.Background()

	cust := &store.Customer{ID: store.NewULID(), Key: "alice", Name: "Alice", CreatedAt: time.Now().UTC()}
	if err := st.CreateCustomer(ctx, cust); err != nil {
		t.Fatalf("create customer: %v", err)
	}
	plan := &store.PlanRow{ID: store.NewULID(), Slug: "pro", Name: "Pro", Entries: "[]", CreatedAt: time.Now().UTC()}
	if err := st.CreatePlan(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	sub := &store.SubscriptionRow{
		ID: store.NewULID(), CustomerID: cust.ID, PlanID: plan.ID,
		StartsAt: time.Now().UTC().Add(-time.Hour), CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateSubscriptionWithMaterialize(ctx, sub, nil); err != nil {
		t.Fatalf("create subscription: %v", err)
	}

	body := authedGet(t, h.Overview, sessions, userID, "/")
	for _, want := range []string{"Plans", "Subscriptions"} {
		if !strings.Contains(body, want) {
			t.Errorf("overview missing %q card", want)
		}
	}
	// Both counts should show 1 (one plan, one active subscription).
	if strings.Count(body, ">1<") < 2 {
		t.Errorf("expected plan and subscription counts of 1 to render; body:\n%s", body)
	}
}

func TestSubscriptionStatus(t *testing.T) {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	ago := now.Add(-time.Hour)
	later := now.Add(time.Hour)

	cases := []struct {
		name string
		row  store.SubscriptionRow
		want string
	}{
		{"active", store.SubscriptionRow{StartsAt: ago}, "active"},
		{"pending", store.SubscriptionRow{StartsAt: later}, "pending"},
		{"expired", store.SubscriptionRow{StartsAt: ago, EndsAt: &ago}, "expired"},
		{"canceled", store.SubscriptionRow{StartsAt: ago, CanceledAt: &ago}, "canceled"},
		{"canceled-beats-expired", store.SubscriptionRow{StartsAt: ago, EndsAt: &ago, CanceledAt: &ago}, "canceled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := subscriptionStatus(&tc.row, now); got != tc.want {
				t.Errorf("status = %q, want %q", got, tc.want)
			}
		})
	}
}
