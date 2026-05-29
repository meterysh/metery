// Package worker drives plan-based grant recurrence. Each tick, walk
// active subscriptions on recurring plans and emit the next cycle's
// grant per plan entry. Idempotent via the
// (subscription_id, entitlement_id, effective_at) unique index.
package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"time"

	meteryv1 "github.com/meterysh/metery/gen/go/metery/v1"
	"github.com/meterysh/metery/internal/store"
	"github.com/sosodev/duration"
	"google.golang.org/protobuf/encoding/protojson"
)

func RunRecurrenceWorker(ctx context.Context, st *store.Store) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Println("worker: tick — running recurrence pass")
			processRecurrence(ctx, st)
		}
	}
}

func processRecurrence(ctx context.Context, st *store.Store) {
	subs, err := st.ListRecurringSubscriptions(ctx)
	if err != nil {
		log.Printf("worker: error listing recurring subscriptions: %v", err)
		return
	}

	log.Printf("worker: evaluating %d recurring subscription(s)", len(subs))

	now := time.Now().UTC().Truncate(time.Second)
	emitted, skipped := 0, 0

	for _, rs := range subs {
		if rs.Plan.RecurrenceInterval == nil {
			continue
		}
		dur, err := duration.Parse(*rs.Plan.RecurrenceInterval)
		if err != nil {
			log.Printf("worker: skipping plan %s — invalid recurrence interval %q: %v", rs.Plan.ID, *rs.Plan.RecurrenceInterval, err)
			skipped++
			continue
		}

		entries, err := planEntriesFromJSON(rs.Plan.Entries)
		if err != nil {
			log.Printf("worker: skipping plan %s — corrupt entries: %v", rs.Plan.ID, err)
			skipped++
			continue
		}

		for _, pe := range entries {
			if pe.Grant == nil {
				continue
			}

			feat, err := st.GetFeature(ctx, pe.FeatureSlug)
			if err != nil {
				log.Printf("worker: skipping entry %s on plan %s — feature lookup failed: %v", pe.FeatureSlug, rs.Plan.ID, err)
				skipped++
				continue
			}
			entID, ok := rs.EntitlementByFeature[feat.ID]
			if !ok {
				continue
			}

			base := rs.Subscription.StartsAt
			last, err := st.GetLatestSubscriptionGrant(ctx, rs.Subscription.ID, entID)
			if err == nil && last != nil {
				base = last.EffectiveAt
			}
			nextTime := shiftTime(base, dur)
			if nextTime.After(now) {
				continue
			}

			priority := int32(100)
			if pe.Grant.Priority != nil {
				priority = *pe.Grant.Priority
			}

			child := &store.GrantRow{
				ID:             store.NewULID(),
				EntitlementID:  entID,
				Amount:         pe.Grant.Amount,
				Priority:       priority,
				EffectiveAt:    nextTime,
				CreatedAt:      now,
				SubscriptionID: &rs.Subscription.ID,
			}
			if pe.Grant.Expiration != nil {
				expDur, err := duration.Parse(pe.Grant.Expiration.Duration)
				if err == nil {
					exp := shiftTime(nextTime, expDur)
					child.ExpiresAt = &exp
				}
			}
			// Rollover lives on entitlement; the worker only writes
			// per-grant lifecycle here (amount/priority/effective/expires).

			if err := st.CreateGrant(ctx, child); err != nil {
				log.Printf("worker: error creating grant for sub %s entitlement %s: %v", rs.Subscription.ID, entID, err)
				continue
			}
			log.Printf("worker: emitted grant %s (sub %s, entitlement %s, effective %s)", child.ID, rs.Subscription.ID, entID, nextTime.Format(time.RFC3339))
			emitted++
		}
	}

	log.Printf("worker: pass complete — emitted %d, skipped %d", emitted, skipped)
}

func shiftTime(t time.Time, d *duration.Duration) time.Time {
	years := int(d.Years)
	months := int(d.Months)
	days := int(d.Weeks)*7 + int(d.Days)

	shifted := t.AddDate(years, months, days)

	hours := time.Duration(d.Hours) * time.Hour
	minutes := time.Duration(d.Minutes) * time.Minute
	seconds := time.Duration(d.Seconds) * time.Second

	totalDuration := hours + minutes + seconds
	return shifted.Add(totalDuration)
}

// planEntriesFromJSON mirrors the service-layer marshalling: each entry
// is its own protojson blob inside a JSON array.
func planEntriesFromJSON(s string) ([]*meteryv1.PlanEntry, error) {
	if s == "" {
		return nil, nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, err
	}
	out := make([]*meteryv1.PlanEntry, 0, len(raw))
	for _, r := range raw {
		e := &meteryv1.PlanEntry{}
		if err := protojson.Unmarshal(bytes.TrimSpace(r), e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
