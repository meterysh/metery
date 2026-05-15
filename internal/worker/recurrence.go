package worker

import (
	"context"
	"log"
	"time"

	"github.com/meterysh/metery/internal/store"
	"github.com/sosodev/duration"
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
	parents, err := st.ListRecurringGrants(ctx)
	if err != nil {
		log.Printf("worker: error listing recurring grants: %v", err)
		return
	}

	log.Printf("worker: evaluating %d recurring grant(s)", len(parents))

	now := time.Now().UTC().Truncate(time.Second)
	emitted, skipped := 0, 0

	for _, p := range parents {
		dur, err := duration.Parse(*p.RecurrenceInterval)
		if err != nil {
			log.Printf("worker: skipping grant %s — invalid recurrence interval %q: %v", p.ID, *p.RecurrenceInterval, err)
			skipped++
			continue
		}

		latestEffective := p.EffectiveAt
		latestChild, err := st.GetLatestChildGrant(ctx, p.ID)
		if err == nil && latestChild != nil {
			latestEffective = latestChild.EffectiveAt
		}

		nextTime := shiftTime(latestEffective, dur)

		if !nextTime.After(now) {
			child := &store.GrantRow{
				ID:            store.NewULID(),
				EntitlementID: p.EntitlementID,
				Amount:        p.Amount,
				Priority:      p.Priority,
				EffectiveAt:   nextTime,
				ParentGrantID: &p.ID,
				CreatedAt:     now,
			}

			if p.ExpiresAt != nil {
				validity := p.ExpiresAt.Sub(p.EffectiveAt)
				exp := nextTime.Add(validity)
				child.ExpiresAt = &exp
			}

			// UNIQUE INDEX (parent_grant_id, effective_at) makes this idempotent.
			if err := st.CreateGrant(ctx, child); err != nil {
				log.Printf("worker: error creating child grant for parent %s: %v", p.ID, err)
			} else {
				log.Printf("worker: emitted grant %s (parent %s, effective %s)", child.ID, p.ID, nextTime.Format(time.RFC3339))
				emitted++
			}
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
