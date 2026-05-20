package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // sqlite driver
	"github.com/meterysh/metery/internal/ledger"
)

// Store provides persistence. We use sqlx to make mapping easier.
type Store struct {
	db *sqlx.DB
}

func New(db *sql.DB, driverName string) *Store {
	return &Store{
		db: sqlx.NewDb(db, driverName),
	}
}

// Model structs
type Customer struct {
	ID            string     `db:"id"`
	Key           string     `db:"key"`
	Name          string     `db:"name"`
	Metadata      *string    `db:"metadata"`
	CreatedAt     time.Time  `db:"created_at"`
	DeactivatedAt *time.Time `db:"deactivated_at"`
}

func (s *Store) CreateCustomer(ctx context.Context, c *Customer) error {
	q := `INSERT INTO customers (id, key, name, metadata, created_at)
	      VALUES (:id, :key, :name, :metadata, :created_at)`
	_, err := s.db.NamedExecContext(ctx, q, c)
	return err
}

func (s *Store) GetCustomer(ctx context.Context, idOrKey string) (*Customer, error) {
	q := `SELECT * FROM customers WHERE id = ? OR key = ?`
	var c Customer
	err := s.db.GetContext(ctx, &c, s.db.Rebind(q), idOrKey, idOrKey)
	return &c, err
}

func (s *Store) ListCustomers(ctx context.Context, limit int, pageToken string) ([]Customer, error) {
	// Simple ULID cursor pagination
	q := `SELECT * FROM customers WHERE id > ? ORDER BY id ASC LIMIT ?`
	var cs []Customer
	err := s.db.SelectContext(ctx, &cs, s.db.Rebind(q), pageToken, limit)
	return cs, err
}

type Meter struct {
	ID            string     `db:"id"`
	Slug          string     `db:"slug"`
	Name          string     `db:"name"`
	Aggregation   string     `db:"aggregation"`
	EventType     string     `db:"event_type"`
	ValueProperty *string    `db:"value_property"`
	CreatedAt     time.Time  `db:"created_at"`
	ArchivedAt    *time.Time `db:"archived_at"`
}

func (s *Store) CreateMeter(ctx context.Context, m *Meter) error {
	q := `INSERT INTO meters (id, slug, name, aggregation, event_type, value_property, created_at)
	      VALUES (:id, :slug, :name, :aggregation, :event_type, :value_property, :created_at)`
	_, err := s.db.NamedExecContext(ctx, q, m)
	return err
}

func (s *Store) GetMeter(ctx context.Context, idOrSlug string) (*Meter, error) {
	q := `SELECT * FROM meters WHERE id = ? OR slug = ?`
	var m Meter
	err := s.db.GetContext(ctx, &m, s.db.Rebind(q), idOrSlug, idOrSlug)
	return &m, err
}

type Feature struct {
	ID         string     `db:"id"`
	Slug       string     `db:"slug"`
	Name       string     `db:"name"`
	MeterID    *string    `db:"meter_id"`
	CreatedAt  time.Time  `db:"created_at"`
	ArchivedAt *time.Time `db:"archived_at"`
}

func (s *Store) CreateFeature(ctx context.Context, f *Feature) error {
	q := `INSERT INTO features (id, slug, name, meter_id, created_at)
	      VALUES (:id, :slug, :name, :meter_id, :created_at)`
	_, err := s.db.NamedExecContext(ctx, q, f)
	return err
}

func (s *Store) GetFeature(ctx context.Context, idOrSlug string) (*Feature, error) {
	q := `SELECT * FROM features WHERE id = ? OR slug = ?`
	var f Feature
	err := s.db.GetContext(ctx, &f, s.db.Rebind(q), idOrSlug, idOrSlug)
	return &f, err
}

type EntitlementRow struct {
	ID                  string     `db:"id"`
	CustomerID          string     `db:"customer_id"`
	FeatureID           string     `db:"feature_id"`
	UsagePeriodDuration *string    `db:"usage_period_duration"`
	UsagePeriodAnchor   *time.Time `db:"usage_period_anchor"`
	CreatedAt           time.Time  `db:"created_at"`
	DeletedAt           *time.Time `db:"deleted_at"`
	// NULL ⇒ manually granted. Non-NULL ⇒ materialised from the named subscription.
	SubscriptionID *string `db:"subscription_id"`
}

func (s *Store) CreateEntitlement(ctx context.Context, e *EntitlementRow) error {
	q := `INSERT INTO entitlements (id, customer_id, feature_id, usage_period_duration, usage_period_anchor, created_at, subscription_id)
	      VALUES (:id, :customer_id, :feature_id, :usage_period_duration, :usage_period_anchor, :created_at, :subscription_id)`
	_, err := s.db.NamedExecContext(ctx, q, e)
	return err
}

func (s *Store) GetEntitlement(ctx context.Context, customerID, featureID string) (*EntitlementRow, error) {
	q := `SELECT * FROM entitlements WHERE customer_id = ? AND feature_id = ? AND deleted_at IS NULL`
	var e EntitlementRow
	err := s.db.GetContext(ctx, &e, s.db.Rebind(q), customerID, featureID)
	return &e, err
}

type GrantRow struct {
	ID                 string     `db:"id"`
	EntitlementID      string     `db:"entitlement_id"`
	Amount             int64      `db:"amount"`
	Priority           int32      `db:"priority"`
	EffectiveAt        time.Time  `db:"effective_at"`
	ExpiresAt          *time.Time `db:"expires_at"`
	RecurrenceInterval *string    `db:"recurrence_interval"`
	RecurrenceAnchor   *time.Time `db:"recurrence_anchor"`
	RolloverMax        *int64     `db:"rollover_max"`
	RolloverType       *string    `db:"rollover_type"`
	ParentGrantID      *string    `db:"parent_grant_id"`
	Metadata           *string    `db:"metadata"`
	CreatedAt          time.Time  `db:"created_at"`
	VoidedAt           *time.Time `db:"voided_at"`
	// NULL ⇒ manually granted. Non-NULL ⇒ materialised from the named subscription.
	SubscriptionID *string `db:"subscription_id"`
}

func (s *Store) CreateGrant(ctx context.Context, g *GrantRow) error {
	q := `INSERT INTO grants (id, entitlement_id, amount, priority, effective_at, expires_at, recurrence_interval, recurrence_anchor, rollover_max, rollover_type, parent_grant_id, metadata, created_at, subscription_id)
	      VALUES (:id, :entitlement_id, :amount, :priority, :effective_at, :expires_at, :recurrence_interval, :recurrence_anchor, :rollover_max, :rollover_type, :parent_grant_id, :metadata, :created_at, :subscription_id)`
	_, err := s.db.NamedExecContext(ctx, q, g)
	return err
}

func (s *Store) ListActiveGrants(ctx context.Context, entitlementID string) ([]ledger.Grant, error) {
	q := `SELECT * FROM grants WHERE entitlement_id = ? AND voided_at IS NULL ORDER BY priority ASC, effective_at ASC`
	var rows []GrantRow
	err := s.db.SelectContext(ctx, &rows, s.db.Rebind(q), entitlementID)
	if err != nil {
		return nil, err
	}

	res := make([]ledger.Grant, len(rows))
	for i, r := range rows {
		rt := ""
		if r.RolloverType != nil {
			rt = *r.RolloverType
		}
		res[i] = ledger.Grant{
			ID:           r.ID,
			Amount:       r.Amount,
			Priority:     r.Priority,
			EffectiveAt:  r.EffectiveAt,
			ExpiresAt:    r.ExpiresAt,
			RolloverMax:  r.RolloverMax,
			RolloverType: rt,
		}
	}
	return res, nil
}

type Event struct {
	ID          string     `db:"id"`
	Customer    string     `db:"customer"`
	Type        string     `db:"type"`
	Time        time.Time  `db:"time"`
	Payload     *string    `db:"payload"` // json text
	CreatedAt   time.Time  `db:"created_at"`
	ProcessedAt *time.Time `db:"processed_at"`
}

func (s *Store) IngestEvent(ctx context.Context, e *Event) error {
	q := `INSERT INTO usage_events (id, customer, type, time, payload, created_at)
	      VALUES (:id, :customer, :type, :time, :payload, :created_at)
	      ON CONFLICT(id) DO NOTHING`
	// sqlite syntax ON CONFLICT requires specific driver support, but it's standard now in recent sqlite/postgres
	_, err := s.db.NamedExecContext(ctx, q, e)
	return err
}

func (s *Store) FetchUsage(ctx context.Context, customerKey string, m *Meter, from, to time.Time) (int64, error) {
	if m.Aggregation == "count" {
		q := `SELECT COALESCE(COUNT(*), 0) FROM usage_events WHERE customer = ? AND type = ? AND time >= ? AND time < ?`
		var usage int64
		err := s.db.GetContext(ctx, &usage, s.db.Rebind(q), customerKey, m.EventType, from, to)
		return usage, err
	}

	if m.ValueProperty == nil {
		return 0, fmt.Errorf("meter %s: aggregation %q requires value_property", m.Slug, m.Aggregation)
	}

	var q, valParam string

	if s.db.DriverName() == "sqlite" {
		// SQLite JSON path: "a.b" → "$.a.b"
		valParam = "$." + *m.ValueProperty
		extract := "json_extract(payload, ?)"
		if m.Aggregation == "unique_count" {
			q = fmt.Sprintf(`SELECT COALESCE(COUNT(DISTINCT %s), 0) FROM usage_events WHERE customer = ? AND type = ? AND time >= ? AND time < ?`, extract)
		} else {
			fn, err := sqlAggFunc(m.Aggregation)
			if err != nil {
				return 0, err
			}
			q = fmt.Sprintf(`SELECT COALESCE(%s(CAST(%s AS NUMERIC)), 0) FROM usage_events WHERE customer = ? AND type = ? AND time >= ? AND time < ?`, fn, extract)
		}
	} else {
		// Postgres JSONB path: "a.b" → "{a,b}" for the #>> operator.
		valParam = "{" + strings.ReplaceAll(*m.ValueProperty, ".", ",") + "}"
		extract := "(payload #>> ?::text[])"
		if m.Aggregation == "unique_count" {
			q = fmt.Sprintf(`SELECT COALESCE(COUNT(DISTINCT %s), 0) FROM usage_events WHERE customer = ? AND type = ? AND time >= ? AND time < ?`, extract)
		} else {
			fn, err := sqlAggFunc(m.Aggregation)
			if err != nil {
				return 0, err
			}
			q = fmt.Sprintf(`SELECT COALESCE(%s((%s)::numeric), 0) FROM usage_events WHERE customer = ? AND type = ? AND time >= ? AND time < ?`, fn, extract)
		}
	}

	var usage float64
	err := s.db.GetContext(ctx, &usage, s.db.Rebind(q), valParam, customerKey, m.EventType, from, to)
	return int64(usage), err
}

func sqlAggFunc(agg string) (string, error) {
	switch agg {
	case "sum":
		return "SUM", nil
	case "avg":
		return "AVG", nil
	case "min":
		return "MIN", nil
	case "max":
		return "MAX", nil
	}
	return "", fmt.Errorf("unsupported aggregation %q", agg)
}

type User struct {
	ID        string    `db:"id"`
	GoogleID  string    `db:"google_id"`
	Email     string    `db:"email"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

func (s *Store) UpsertUser(ctx context.Context, googleID, email, name string) (*User, error) {
	q := `INSERT INTO users (id, google_id, email, name, created_at)
	      VALUES (?, ?, ?, ?, ?)
	      ON CONFLICT (google_id) DO UPDATE SET email = excluded.email, name = excluded.name`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), NewULID(), googleID, email, name, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return s.GetUserByGoogleID(ctx, googleID)
}

func (s *Store) GetUserByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u, s.db.Rebind(`SELECT * FROM users WHERE id = ?`), id)
	return &u, err
}

func (s *Store) GetUserByGoogleID(ctx context.Context, googleID string) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u, s.db.Rebind(`SELECT * FROM users WHERE google_id = ?`), googleID)
	return &u, err
}

func NewULID() string {
	return strings.ToLower(ulid.Make().String())
}

func (s *Store) ListRecurringGrants(ctx context.Context) ([]GrantRow, error) {
	q := "SELECT * FROM grants WHERE recurrence_interval IS NOT NULL AND voided_at IS NULL AND parent_grant_id IS NULL"
	var rows []GrantRow
	err := s.db.SelectContext(ctx, &rows, q)
	return rows, err
}

func (s *Store) GetLatestChildGrant(ctx context.Context, parentID string) (*GrantRow, error) {
	q := "SELECT * FROM grants WHERE parent_grant_id = ? ORDER BY effective_at DESC LIMIT 1"
	var row GrantRow
	err := s.db.GetContext(ctx, &row, s.db.Rebind(q), parentID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) ListMeters(ctx context.Context, includeArchived bool, limit int, after string) ([]Meter, error) {
	var q string
	if includeArchived {
		q = `SELECT * FROM meters WHERE id > ? ORDER BY id ASC LIMIT ?`
	} else {
		q = `SELECT * FROM meters WHERE id > ? AND archived_at IS NULL ORDER BY id ASC LIMIT ?`
	}
	var ms []Meter
	err := s.db.SelectContext(ctx, &ms, s.db.Rebind(q), after, limit)
	return ms, err
}

func (s *Store) ArchiveMeter(ctx context.Context, idOrSlug string) error {
	q := `UPDATE meters SET archived_at = ? WHERE (id = ? OR slug = ?) AND archived_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), idOrSlug, idOrSlug)
	return err
}

func (s *Store) ListFeatures(ctx context.Context, includeArchived bool, limit int, after string) ([]Feature, error) {
	var q string
	if includeArchived {
		q = `SELECT * FROM features WHERE id > ? ORDER BY id ASC LIMIT ?`
	} else {
		q = `SELECT * FROM features WHERE id > ? AND archived_at IS NULL ORDER BY id ASC LIMIT ?`
	}
	var fs []Feature
	err := s.db.SelectContext(ctx, &fs, s.db.Rebind(q), after, limit)
	return fs, err
}

func (s *Store) ArchiveFeature(ctx context.Context, idOrSlug string) error {
	q := `UPDATE features SET archived_at = ? WHERE (id = ? OR slug = ?) AND archived_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), idOrSlug, idOrSlug)
	return err
}

func (s *Store) ListEntitlements(ctx context.Context, customerID string, limit int, after string) ([]EntitlementRow, error) {
	q := `SELECT * FROM entitlements WHERE customer_id = ? AND id > ? AND deleted_at IS NULL ORDER BY id ASC LIMIT ?`
	var es []EntitlementRow
	err := s.db.SelectContext(ctx, &es, s.db.Rebind(q), customerID, after, limit)
	return es, err
}

func (s *Store) DeleteEntitlement(ctx context.Context, customerID, featureID string) error {
	q := `UPDATE entitlements SET deleted_at = ? WHERE customer_id = ? AND feature_id = ? AND deleted_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), customerID, featureID)
	return err
}

func (s *Store) ResetEntitlement(ctx context.Context, entitlementID string, at time.Time) error {
	q := `UPDATE entitlements SET usage_period_anchor = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), at, entitlementID)
	return err
}

func (s *Store) ListGrants(ctx context.Context, entitlementID string, includeVoided bool, limit int, after string) ([]GrantRow, error) {
	var q string
	if includeVoided {
		q = `SELECT * FROM grants WHERE entitlement_id = ? AND id > ? ORDER BY id ASC LIMIT ?`
	} else {
		q = `SELECT * FROM grants WHERE entitlement_id = ? AND id > ? AND voided_at IS NULL ORDER BY id ASC LIMIT ?`
	}
	var gs []GrantRow
	err := s.db.SelectContext(ctx, &gs, s.db.Rebind(q), entitlementID, after, limit)
	return gs, err
}

func (s *Store) VoidGrant(ctx context.Context, id string) error {
	q := `UPDATE grants SET voided_at = ? WHERE id = ? AND voided_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), id)
	return err
}

func (s *Store) UpdateCustomer(ctx context.Context, idOrKey string, name string) (*Customer, error) {
	q := `UPDATE customers SET name = ? WHERE (id = ? OR key = ?) AND deactivated_at IS NULL`
	if _, err := s.db.ExecContext(ctx, s.db.Rebind(q), name, idOrKey, idOrKey); err != nil {
		return nil, err
	}
	return s.GetCustomer(ctx, idOrKey)
}

func (s *Store) DeactivateCustomer(ctx context.Context, idOrKey string) error {
	q := `UPDATE customers SET deactivated_at = ? WHERE (id = ? OR key = ?) AND deactivated_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), idOrKey, idOrKey)
	return err
}

type SnapshotRow struct {
	EntitlementID string    `db:"entitlement_id"`
	AsOf          time.Time `db:"as_of"`
	Balance       int64     `db:"balance"`
	PerGrantState string    `db:"per_grant_state"` // JSON: [{grant_id, remaining}]
}

// GetLatestSnapshot returns the most recent snapshot at or before before, or nil if none exists.
func (s *Store) GetLatestSnapshot(ctx context.Context, entitlementID string, before time.Time) (*ledger.Snapshot, error) {
	q := `SELECT * FROM balance_snapshots WHERE entitlement_id = ? AND as_of <= ? ORDER BY as_of DESC LIMIT 1`
	var row SnapshotRow
	err := s.db.GetContext(ctx, &row, s.db.Rebind(q), entitlementID, before)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state []ledger.GrantState
	if err := json.Unmarshal([]byte(row.PerGrantState), &state); err != nil {
		return nil, err
	}
	return &ledger.Snapshot{
		AsOf:          row.AsOf,
		Balance:       row.Balance,
		PerGrantState: state,
	}, nil
}

// SaveSnapshots persists snapshots at period boundaries. Concurrent duplicate writes are silently ignored.
func (s *Store) SaveSnapshots(ctx context.Context, snaps []ledger.Snapshot, entitlementID string) error {
	for _, snap := range snaps {
		b, err := json.Marshal(snap.PerGrantState)
		if err != nil {
			return err
		}
		row := SnapshotRow{
			EntitlementID: entitlementID,
			AsOf:          snap.AsOf,
			Balance:       snap.Balance,
			PerGrantState: string(b),
		}
		q := `INSERT INTO balance_snapshots (entitlement_id, as_of, balance, per_grant_state) VALUES (:entitlement_id, :as_of, :balance, :per_grant_state) ON CONFLICT (entitlement_id, as_of) DO NOTHING`
		if _, err := s.db.NamedExecContext(ctx, q, row); err != nil {
			return err
		}
	}
	return nil
}

// ─── Plans ─────────────────────────────────────────────────────────

type PlanRow struct {
	ID         string     `db:"id"`
	Slug       string     `db:"slug"`
	Name       string     `db:"name"`
	Entries    string     `db:"entries"` // JSON: []PlanEntry, marshalled by service layer
	Metadata   *string    `db:"metadata"`
	CreatedAt  time.Time  `db:"created_at"`
	ArchivedAt *time.Time `db:"archived_at"`
}

func (s *Store) CreatePlan(ctx context.Context, p *PlanRow) error {
	q := `INSERT INTO plans (id, slug, name, entries, metadata, created_at)
	      VALUES (:id, :slug, :name, :entries, :metadata, :created_at)`
	_, err := s.db.NamedExecContext(ctx, q, p)
	return err
}

func (s *Store) GetPlan(ctx context.Context, idOrSlug string) (*PlanRow, error) {
	q := `SELECT * FROM plans WHERE id = ? OR slug = ?`
	var p PlanRow
	err := s.db.GetContext(ctx, &p, s.db.Rebind(q), idOrSlug, idOrSlug)
	return &p, err
}

func (s *Store) ListPlans(ctx context.Context, includeArchived bool, limit int, after string) ([]PlanRow, error) {
	var q string
	if includeArchived {
		q = `SELECT * FROM plans WHERE id > ? ORDER BY id ASC LIMIT ?`
	} else {
		q = `SELECT * FROM plans WHERE id > ? AND archived_at IS NULL ORDER BY id ASC LIMIT ?`
	}
	var ps []PlanRow
	err := s.db.SelectContext(ctx, &ps, s.db.Rebind(q), after, limit)
	return ps, err
}

func (s *Store) ArchivePlan(ctx context.Context, idOrSlug string) error {
	q := `UPDATE plans SET archived_at = ? WHERE (id = ? OR slug = ?) AND archived_at IS NULL`
	_, err := s.db.ExecContext(ctx, s.db.Rebind(q), time.Now().UTC().Truncate(time.Second), idOrSlug, idOrSlug)
	return err
}

// ─── Subscriptions ─────────────────────────────────────────────────

type SubscriptionRow struct {
	ID         string     `db:"id"`
	CustomerID string     `db:"customer_id"`
	PlanID     string     `db:"plan_id"`
	StartsAt   time.Time  `db:"starts_at"`
	EndsAt     *time.Time `db:"ends_at"`
	CanceledAt *time.Time `db:"canceled_at"`
	Metadata   *string    `db:"metadata"`
	CreatedAt  time.Time  `db:"created_at"`
}

// SubscriptionEntry is the prepared, server-resolved form of a plan entry —
// ready to materialise. The service layer turns proto PlanEntry + plan +
// subscription start time into one of these per entry.
type SubscriptionEntry struct {
	FeatureID           string
	UsagePeriodDuration *string
	UsagePeriodAnchor   *time.Time
	Grant               *MaterializeGrant
}

// MaterializeGrant carries the absolute, resolved fields for a grant created
// as part of subscribe / change. ID / EntitlementID / SubscriptionID /
// EffectiveAt / CreatedAt are filled in by the store.
type MaterializeGrant struct {
	Amount             int64
	Priority           int32
	ExpiresAt          *time.Time
	RecurrenceInterval *string
	RecurrenceAnchor   *time.Time
	RolloverMax        *int64
	RolloverType       *string
	Metadata           *string
}

func (s *Store) GetSubscription(ctx context.Context, id string) (*SubscriptionRow, error) {
	q := `SELECT * FROM subscriptions WHERE id = ?`
	var sub SubscriptionRow
	err := s.db.GetContext(ctx, &sub, s.db.Rebind(q), id)
	return &sub, err
}

// ListSubscriptions returns subscriptions, optionally scoped to one customer.
// customerID == "" lists across all customers.
func (s *Store) ListSubscriptions(ctx context.Context, customerID string, includeCanceled bool, limit int, after string) ([]SubscriptionRow, error) {
	conds := []string{"id > ?"}
	args := []any{after}
	if customerID != "" {
		conds = append(conds, "customer_id = ?")
		args = append(args, customerID)
	}
	if !includeCanceled {
		conds = append(conds, "canceled_at IS NULL")
	}
	q := `SELECT * FROM subscriptions WHERE ` + strings.Join(conds, " AND ") + ` ORDER BY id ASC LIMIT ?`
	args = append(args, limit)
	var subs []SubscriptionRow
	err := s.db.SelectContext(ctx, &subs, s.db.Rebind(q), args...)
	return subs, err
}

// CreateSubscriptionWithMaterialize inserts the subscription row and creates
// the entitlements + grants implied by `entries`, in a single transaction.
// Entitlements are reused if (customer_id, feature_id) already has an active
// row — only newly-created entitlements carry subscription_id, so manual
// entitlements stay manual.
func (s *Store) CreateSubscriptionWithMaterialize(ctx context.Context, sub *SubscriptionRow, entries []SubscriptionEntry) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.NamedExecContext(ctx, `INSERT INTO subscriptions (id, customer_id, plan_id, starts_at, ends_at, canceled_at, metadata, created_at)
	      VALUES (:id, :customer_id, :plan_id, :starts_at, :ends_at, :canceled_at, :metadata, :created_at)`, sub); err != nil {
		return err
	}

	if err := materializeEntries(ctx, tx, sub, entries); err != nil {
		return err
	}

	return tx.Commit()
}

// CancelSubscription sets canceled_at (and ends_at if unset) on the
// subscription and voids any active grants it owns.
func (s *Store) CancelSubscription(ctx context.Context, id string, at time.Time) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE subscriptions SET canceled_at = ?, ends_at = COALESCE(ends_at, ?) WHERE id = ? AND canceled_at IS NULL`),
		at, at, id); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE grants SET voided_at = ? WHERE subscription_id = ? AND voided_at IS NULL`),
		at, id); err != nil {
		return err
	}

	return tx.Commit()
}

// ChangeSubscription voids the current subscription's active grants, updates
// plan_id, and materialises the new plan's entries from effectiveAt.
// Entitlements are preserved (reused when the new plan covers a feature
// already entitled).
func (s *Store) ChangeSubscription(ctx context.Context, sub *SubscriptionRow, newPlanID string, effectiveAt time.Time, entries []SubscriptionEntry) error {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE grants SET voided_at = ? WHERE subscription_id = ? AND voided_at IS NULL`),
		effectiveAt, sub.ID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, tx.Rebind(
		`UPDATE subscriptions SET plan_id = ? WHERE id = ?`),
		newPlanID, sub.ID); err != nil {
		return err
	}

	subForMaterialize := *sub
	subForMaterialize.PlanID = newPlanID
	subForMaterialize.StartsAt = effectiveAt
	if err := materializeEntries(ctx, tx, &subForMaterialize, entries); err != nil {
		return err
	}

	return tx.Commit()
}

func materializeEntries(ctx context.Context, tx *sqlx.Tx, sub *SubscriptionRow, entries []SubscriptionEntry) error {
	for _, e := range entries {
		var entID string
		err := tx.GetContext(ctx, &entID,
			tx.Rebind(`SELECT id FROM entitlements WHERE customer_id = ? AND feature_id = ? AND deleted_at IS NULL`),
			sub.CustomerID, e.FeatureID)
		if errors.Is(err, sql.ErrNoRows) {
			entID = NewULID()
			ent := EntitlementRow{
				ID:                  entID,
				CustomerID:          sub.CustomerID,
				FeatureID:           e.FeatureID,
				UsagePeriodDuration: e.UsagePeriodDuration,
				UsagePeriodAnchor:   e.UsagePeriodAnchor,
				CreatedAt:           sub.CreatedAt,
				SubscriptionID:      &sub.ID,
			}
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO entitlements (id, customer_id, feature_id, usage_period_duration, usage_period_anchor, created_at, subscription_id)
		      VALUES (:id, :customer_id, :feature_id, :usage_period_duration, :usage_period_anchor, :created_at, :subscription_id)`, ent); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}

		if e.Grant != nil {
			grant := GrantRow{
				ID:                 NewULID(),
				EntitlementID:      entID,
				Amount:             e.Grant.Amount,
				Priority:           e.Grant.Priority,
				EffectiveAt:        sub.StartsAt,
				ExpiresAt:          e.Grant.ExpiresAt,
				RecurrenceInterval: e.Grant.RecurrenceInterval,
				RecurrenceAnchor:   e.Grant.RecurrenceAnchor,
				RolloverMax:        e.Grant.RolloverMax,
				RolloverType:       e.Grant.RolloverType,
				Metadata:           e.Grant.Metadata,
				CreatedAt:          sub.CreatedAt,
				SubscriptionID:     &sub.ID,
			}
			if _, err := tx.NamedExecContext(ctx, `INSERT INTO grants (id, entitlement_id, amount, priority, effective_at, expires_at, recurrence_interval, recurrence_anchor, rollover_max, rollover_type, parent_grant_id, metadata, created_at, subscription_id)
		      VALUES (:id, :entitlement_id, :amount, :priority, :effective_at, :expires_at, :recurrence_interval, :recurrence_anchor, :rollover_max, :rollover_type, :parent_grant_id, :metadata, :created_at, :subscription_id)`, grant); err != nil {
				return err
			}
		}
	}
	return nil
}
