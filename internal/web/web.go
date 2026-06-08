package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/meterysh/metery/internal/auth"
	"github.com/meterysh/metery/internal/store"
)

//go:embed templates/*.html
var templatesFS embed.FS

type Handler struct {
	st       *store.Store
	sessions *auth.SessionManager
	tmpl     *template.Template            // standalone pages
	pages    map[string]*template.Template // layout-composed pages
}

func NewHandler(st *store.Store, sessions *auth.SessionManager) *Handler {
	h := &Handler{
		st:       st,
		sessions: sessions,
		tmpl:     template.Must(template.ParseFS(templatesFS, "templates/login.html")),
		pages:    map[string]*template.Template{},
	}
	for _, p := range []string{"index", "meters", "features", "customers", "customer_detail", "meter_detail", "feature_detail", "plans", "plan_detail", "subscriptions"} {
		h.pages[p] = template.Must(template.ParseFS(
			templatesFS,
			"templates/layout.html",
			"templates/"+p+".html",
		))
	}
	return h
}

type layoutData struct {
	ActiveTab string
	Title     string
	User      *store.User
}

// Overview

type overviewData struct {
	layoutData
	CustomerCount     int
	MeterCount        int
	FeatureCount      int
	PlanCount         int
	SubscriptionCount int
}

func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := overviewData{layoutData: layoutData{ActiveTab: "overview", Title: "Overview", User: user}}
	if cs, err := h.st.ListCustomers(r.Context(), 1000, ""); err == nil {
		data.CustomerCount = len(cs)
	}
	if ms, err := h.st.ListMeters(r.Context(), false, 1000, ""); err == nil {
		data.MeterCount = len(ms)
	}
	if fs, err := h.st.ListFeatures(r.Context(), false, 1000, ""); err == nil {
		data.FeatureCount = len(fs)
	}
	if ps, err := h.st.ListPlans(r.Context(), false, 1000, ""); err == nil {
		data.PlanCount = len(ps)
	}
	if subs, err := h.st.ListSubscriptions(r.Context(), "", "", false, 1000, ""); err == nil {
		data.SubscriptionCount = len(subs)
	}
	h.render(w, "index", data)
}

// Meters

type meterRow struct {
	ID            string
	Slug          string
	Name          string
	Aggregation   string
	EventType     string
	ValueProperty string
	CreatedAt     string
}

type metersData struct {
	layoutData
	Meters []meterRow
}

func (h *Handler) MetersPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := metersData{layoutData: layoutData{ActiveTab: "meters", Title: "Meters", User: user}}
	if ms, err := h.st.ListMeters(r.Context(), false, 50, ""); err == nil {
		for _, m := range ms {
			vp := ""
			if m.ValueProperty != nil {
				vp = *m.ValueProperty
			}
			data.Meters = append(data.Meters, meterRow{
				ID:            m.ID,
				Slug:          m.Slug,
				Name:          m.Name,
				Aggregation:   m.Aggregation,
				EventType:     m.EventType,
				ValueProperty: vp,
				CreatedAt:     m.CreatedAt.Local().Format(time.DateTime),
			})
		}
	}
	h.render(w, "meters", data)
}

// Features

type featureRow struct {
	ID        string
	Slug      string
	Name      string
	Type      string // "metered" or "boolean"
	CreatedAt string
}

type featuresData struct {
	layoutData
	Features []featureRow
}

func (h *Handler) FeaturesPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := featuresData{layoutData: layoutData{ActiveTab: "features", Title: "Features", User: user}}
	if fs, err := h.st.ListFeatures(r.Context(), false, 50, ""); err == nil {
		for _, f := range fs {
			t := "boolean"
			if f.MeterID != nil {
				t = "metered"
			}
			data.Features = append(data.Features, featureRow{
				ID:        f.ID,
				Slug:      f.Slug,
				Name:      f.Name,
				Type:      t,
				CreatedAt: f.CreatedAt.Local().Format(time.DateTime),
			})
		}
	}
	h.render(w, "features", data)
}

// Customers

type customerRow struct {
	ID        string
	Key       string
	Name      string
	CreatedAt string
	Active    bool
}

type customersData struct {
	layoutData
	Customers []customerRow
}

func (h *Handler) CustomersPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := customersData{layoutData: layoutData{ActiveTab: "customers", Title: "Customers", User: user}}
	if cs, err := h.st.ListCustomers(r.Context(), 50, ""); err == nil {
		for _, c := range cs {
			data.Customers = append(data.Customers, customerRow{
				ID:        c.ID,
				Key:       c.Key,
				Name:      c.Name,
				CreatedAt: c.CreatedAt.Local().Format(time.DateTime),
				Active:    c.DeactivatedAt == nil,
			})
		}
	}
	h.render(w, "customers", data)
}

// Customer detail

type grantView struct {
	ID          string
	Amount      int64
	Priority    int32
	EffectiveAt string
	ExpiresAt   string
	Voided      bool
	CreatedAt   string
}

type entitlementView struct {
	ID          string
	FeatureSlug string
	FeatureName string
	UsagePeriod string
	CreatedAt   string
	Deleted     bool
	Grants      []grantView
}

type customerDetailData struct {
	layoutData
	CustomerID    string
	CustomerKey   string
	CustomerName  string
	CreatedAt     string
	DeactivatedAt string
	Active        bool
	Entitlements  []entitlementView
}

func (h *Handler) CustomerDetail(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	c, err := h.st.GetCustomer(r.Context(), r.PathValue("id_or_key"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := customerDetailData{
		layoutData:   layoutData{ActiveTab: "customers", Title: c.Name, User: user},
		CustomerID:   c.ID,
		CustomerKey:  c.Key,
		CustomerName: c.Name,
		CreatedAt:    c.CreatedAt.Local().Format(time.DateTime),
		Active:       c.DeactivatedAt == nil,
	}
	if c.DeactivatedAt != nil {
		data.DeactivatedAt = c.DeactivatedAt.Local().Format(time.DateTime)
	}

	featureByID := map[string]store.Feature{}
	if fs, err := h.st.ListFeatures(r.Context(), true, 1000, ""); err == nil {
		for _, f := range fs {
			featureByID[f.ID] = f
		}
	}

	if ents, err := h.st.ListEntitlements(r.Context(), c.ID, 100, ""); err == nil {
		for _, e := range ents {
			ev := entitlementView{
				ID:        e.ID,
				CreatedAt: e.CreatedAt.Local().Format(time.DateTime),
				Deleted:   e.DeletedAt != nil,
			}
			if f, ok := featureByID[e.FeatureID]; ok {
				ev.FeatureSlug = f.Slug
				ev.FeatureName = f.Name
			} else {
				ev.FeatureSlug = e.FeatureID
			}
			if e.UsagePeriodDuration != nil {
				ev.UsagePeriod = *e.UsagePeriodDuration
			}
			if grants, err := h.st.ListGrants(r.Context(), e.ID, true, 100, ""); err == nil {
				for _, g := range grants {
					gv := grantView{
						ID:          g.ID,
						Amount:      g.Amount,
						Priority:    g.Priority,
						EffectiveAt: g.EffectiveAt.Local().Format(time.DateTime),
						Voided:      g.VoidedAt != nil,
						CreatedAt:   g.CreatedAt.Local().Format(time.DateTime),
					}
					if g.ExpiresAt != nil {
						gv.ExpiresAt = g.ExpiresAt.Local().Format(time.DateTime)
					}
					ev.Grants = append(ev.Grants, gv)
				}
			}
			data.Entitlements = append(data.Entitlements, ev)
		}
	}
	h.render(w, "customer_detail", data)
}

// Meter detail

type meterFeatureRow struct {
	Slug      string
	Name      string
	CreatedAt string
}

type meterDetailData struct {
	layoutData
	ID            string
	Slug          string
	Name          string
	Aggregation   string
	EventType     string
	ValueProperty string
	CreatedAt     string
	ArchivedAt    string
	Archived      bool
	Features      []meterFeatureRow
}

func (h *Handler) MeterDetail(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	m, err := h.st.GetMeter(r.Context(), r.PathValue("id_or_slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := meterDetailData{
		layoutData:  layoutData{ActiveTab: "meters", Title: m.Name, User: user},
		ID:          m.ID,
		Slug:        m.Slug,
		Name:        m.Name,
		Aggregation: m.Aggregation,
		EventType:   m.EventType,
		CreatedAt:   m.CreatedAt.Local().Format(time.DateTime),
		Archived:    m.ArchivedAt != nil,
	}
	if m.ValueProperty != nil {
		data.ValueProperty = *m.ValueProperty
	}
	if m.ArchivedAt != nil {
		data.ArchivedAt = m.ArchivedAt.Local().Format(time.DateTime)
	}
	if fs, err := h.st.ListFeatures(r.Context(), true, 1000, ""); err == nil {
		for _, f := range fs {
			if f.MeterID != nil && *f.MeterID == m.ID {
				data.Features = append(data.Features, meterFeatureRow{
					Slug:      f.Slug,
					Name:      f.Name,
					CreatedAt: f.CreatedAt.Local().Format(time.DateTime),
				})
			}
		}
	}
	h.render(w, "meter_detail", data)
}

// Feature detail

type featureDetailData struct {
	layoutData
	ID         string
	Slug       string
	Name       string
	Type       string
	MeterSlug  string
	MeterName  string
	CreatedAt  string
	ArchivedAt string
	Archived   bool
}

func (h *Handler) FeatureDetail(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	f, err := h.st.GetFeature(r.Context(), r.PathValue("id_or_slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := featureDetailData{
		layoutData: layoutData{ActiveTab: "features", Title: f.Name, User: user},
		ID:         f.ID,
		Slug:       f.Slug,
		Name:       f.Name,
		Type:       "boolean",
		CreatedAt:  f.CreatedAt.Local().Format(time.DateTime),
		Archived:   f.ArchivedAt != nil,
	}
	if f.MeterID != nil {
		data.Type = "metered"
		if m, err := h.st.GetMeter(r.Context(), *f.MeterID); err == nil {
			data.MeterSlug = m.Slug
			data.MeterName = m.Name
		}
	}
	if f.ArchivedAt != nil {
		data.ArchivedAt = f.ArchivedAt.Local().Format(time.DateTime)
	}
	h.render(w, "feature_detail", data)
}

// Plans

type planRow struct {
	ID         string
	Slug       string
	Name       string
	Features   string // comma-joined feature slugs from entries
	Recurrence string // ISO-8601 interval; empty for one-shot plans
	CreatedAt  string
}

type plansData struct {
	layoutData
	Plans []planRow
}

func (h *Handler) PlansPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := plansData{layoutData: layoutData{ActiveTab: "plans", Title: "Plans", User: user}}
	if ps, err := h.st.ListPlans(r.Context(), false, 50, ""); err == nil {
		for _, p := range ps {
			row := planRow{
				ID:        p.ID,
				Slug:      p.Slug,
				Name:      p.Name,
				Features:  strings.Join(planFeatureSlugs(p.Entries), ", "),
				CreatedAt: p.CreatedAt.Local().Format(time.DateTime),
			}
			if p.RecurrenceInterval != nil {
				row.Recurrence = *p.RecurrenceInterval
			}
			data.Plans = append(data.Plans, row)
		}
	}
	h.render(w, "plans", data)
}

// planFeatureSlugs extracts the feature slugs from a plan's stored entries JSON
// for display, without depending on the proto types.
func planFeatureSlugs(entriesJSON string) []string {
	if entriesJSON == "" {
		return nil
	}
	var entries []struct {
		FeatureSlug string `json:"feature_slug"`
	}
	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		return nil
	}
	slugs := make([]string, 0, len(entries))
	for _, e := range entries {
		slugs = append(slugs, e.FeatureSlug)
	}
	return slugs
}

// Plan detail

type planEntryView struct {
	FeatureSlug string
	UsagePeriod string // ISO-8601 duration; empty for boolean / no windowing
	GrantAmount string // int64 as string (protojson encoding); empty for boolean
	Priority    string // empty when grant absent or priority unset
	Expiration  string // ISO-8601 duration; empty if none
	Rollover    string // max_amount as string; empty ⇒ uncapped carryover
}

type planDetailData struct {
	layoutData
	ID            string
	Slug          string
	Name          string
	Recurrence    string
	RecurrenceAt  string
	CreatedAt     string
	ArchivedAt    string
	Archived      bool
	Entries       []planEntryView
	SubCount      int
	ActiveSubs    int
}

func (h *Handler) PlanDetail(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	p, err := h.st.GetPlan(r.Context(), r.PathValue("id_or_slug"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data := planDetailData{
		layoutData: layoutData{ActiveTab: "plans", Title: p.Name, User: user},
		ID:         p.ID,
		Slug:       p.Slug,
		Name:       p.Name,
		CreatedAt:  p.CreatedAt.Local().Format(time.DateTime),
		Archived:   p.ArchivedAt != nil,
	}
	if p.RecurrenceInterval != nil {
		data.Recurrence = *p.RecurrenceInterval
	}
	if p.RecurrenceAnchor != nil {
		data.RecurrenceAt = p.RecurrenceAnchor.Local().Format(time.DateTime)
	}
	if p.ArchivedAt != nil {
		data.ArchivedAt = p.ArchivedAt.Local().Format(time.DateTime)
	}
	data.Entries = planEntryViews(p.Entries)

	// Subscription counts for this plan — a count, not a list: the
	// subscriptions table is unbounded, and the /subscriptions page (which
	// carries the plan column) is where you actually browse them.
	if total, active, err := h.st.CountSubscriptionsForPlan(r.Context(), p.ID); err == nil {
		data.SubCount = total
		data.ActiveSubs = active
	}
	h.render(w, "plan_detail", data)
}

// planEntryViews decodes a plan's stored entries JSON into display rows.
// Entries are protojson-encoded (snake_case names, int64 as string), so the
// shape mirrors meterv1.PlanEntry without depending on the proto types.
func planEntryViews(entriesJSON string) []planEntryView {
	if entriesJSON == "" {
		return nil
	}
	var entries []struct {
		FeatureSlug string `json:"feature_slug"`
		UsagePeriod *struct {
			Duration string `json:"duration"`
		} `json:"usage_period"`
		Grant *struct {
			Amount     string `json:"amount"`
			Priority   *int   `json:"priority"`
			Expiration *struct {
				Duration string `json:"duration"`
			} `json:"expiration"`
		} `json:"grant"`
		Rollover *struct {
			MaxAmount string `json:"max_amount"`
		} `json:"rollover"`
	}
	if err := json.Unmarshal([]byte(entriesJSON), &entries); err != nil {
		return nil
	}
	views := make([]planEntryView, 0, len(entries))
	for _, e := range entries {
		v := planEntryView{FeatureSlug: e.FeatureSlug}
		if e.UsagePeriod != nil {
			v.UsagePeriod = e.UsagePeriod.Duration
		}
		if e.Grant != nil {
			v.GrantAmount = e.Grant.Amount
			if e.Grant.Priority != nil {
				v.Priority = strconv.Itoa(*e.Grant.Priority)
			}
			if e.Grant.Expiration != nil {
				v.Expiration = e.Grant.Expiration.Duration
			}
		}
		if e.Rollover != nil {
			v.Rollover = e.Rollover.MaxAmount
		}
		views = append(views, v)
	}
	return views
}

// Subscriptions

type subscriptionRow struct {
	ID          string
	CustomerKey string
	PlanSlug    string
	Status      string
	StartsAt    string
	CreatedAt   string
}

type subscriptionsData struct {
	layoutData
	Subscriptions []subscriptionRow
	FilterPlan    string // slug of the plan filter in effect; empty ⇒ unfiltered
}

func (h *Handler) SubscriptionsPage(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := subscriptionsData{layoutData: layoutData{ActiveTab: "subscriptions", Title: "Subscriptions", User: user}}

	// Optional ?plan=<id_or_slug> filter. Resolve to the plan's ULID for the
	// query; an unrecognised value filters to a non-existent id ⇒ empty list.
	planID := ""
	if q := r.URL.Query().Get("plan"); q != "" {
		if p, err := h.st.GetPlan(r.Context(), q); err == nil {
			planID = p.ID
			data.FilterPlan = p.Slug
		} else {
			planID = q
			data.FilterPlan = q
		}
	}

	custKeyByID := map[string]string{}
	if cs, err := h.st.ListCustomers(r.Context(), 1000, ""); err == nil {
		for _, c := range cs {
			custKeyByID[c.ID] = c.Key
		}
	}
	planSlugByID := map[string]string{}
	if ps, err := h.st.ListPlans(r.Context(), true, 1000, ""); err == nil {
		for _, p := range ps {
			planSlugByID[p.ID] = p.Slug
		}
	}

	now := time.Now()
	if subs, err := h.st.ListSubscriptions(r.Context(), "", planID, true, 50, ""); err == nil {
		for _, s := range subs {
			ckey := custKeyByID[s.CustomerID]
			if ckey == "" {
				ckey = s.CustomerID
			}
			pslug := planSlugByID[s.PlanID]
			if pslug == "" {
				pslug = s.PlanID
			}
			data.Subscriptions = append(data.Subscriptions, subscriptionRow{
				ID:          s.ID,
				CustomerKey: ckey,
				PlanSlug:    pslug,
				Status:      subscriptionStatus(&s, now),
				StartsAt:    s.StartsAt.Local().Format(time.DateTime),
				CreatedAt:   s.CreatedAt.Local().Format(time.DateTime),
			})
		}
	}
	h.render(w, "subscriptions", data)
}

// subscriptionStatus derives display status from the row's timestamps —
// there is no stored status column (see subscription.proto).
func subscriptionStatus(s *store.SubscriptionRow, now time.Time) string {
	switch {
	case s.CanceledAt != nil:
		return "canceled"
	case s.EndsAt != nil && !now.Before(*s.EndsAt):
		return "expired"
	case now.Before(s.StartsAt):
		return "pending"
	default:
		return "active"
	}
}

// Auth helpers

func (h *Handler) currentUser(r *http.Request) *store.User {
	id := h.sessions.UserID(r)
	if id == "" {
		return nil
	}
	u, err := h.st.GetUserByID(r.Context(), id)
	if err != nil {
		return nil
	}
	return u
}

func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) *store.User {
	u := h.currentUser(r)
	if u == nil {
		h.renderStandalone(w, "login.html", nil)
		return nil
	}
	return u
}

// Render helpers

func (h *Handler) render(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	t, ok := h.pages[name]
	if !ok {
		http.Error(w, "unknown page", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(w, "layout.html", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (h *Handler) renderStandalone(w http.ResponseWriter, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
