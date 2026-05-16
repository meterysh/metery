package web

import (
	"embed"
	"html/template"
	"net/http"
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
	for _, p := range []string{"index", "meters", "features", "customers", "customer_detail", "meter_detail", "feature_detail"} {
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
	CustomerCount int
	MeterCount    int
	FeatureCount  int
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
	Recurrence  string
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
					if g.RecurrenceInterval != nil {
						gv.Recurrence = *g.RecurrenceInterval
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
