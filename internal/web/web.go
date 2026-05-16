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
	for _, p := range []string{"index", "features", "customers"} {
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

// Meters (index)

type meterRow struct {
	ID            string
	Slug          string
	Name          string
	Aggregation   string
	EventType     string
	ValueProperty string
	CreatedAt     string
}

type indexData struct {
	layoutData
	Meters []meterRow
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	user := h.requireUser(w, r)
	if user == nil {
		return
	}
	data := indexData{layoutData: layoutData{ActiveTab: "meters", User: user}}
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
	h.render(w, "index", data)
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
	data := featuresData{layoutData: layoutData{ActiveTab: "features", User: user}}
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
	data := customersData{layoutData: layoutData{ActiveTab: "customers", User: user}}
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
