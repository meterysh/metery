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
			processRecurrence(ctx, st)
		}
	}
}

func processRecurrence(ctx context.Context, st *store.Store) {
	// 1. Fetch recurring grants that need a child.
	// For simplicity in v0: we ask the store to find recurring grants whose NextEffectiveAt <= time.Now().UTC().Truncate(time.Second)
	// Since that requires complex SQL math (adding duration strings to dates),
	// we fetch ALL active recurring grants, and evaluate in Go.
	
	parents, err := st.ListRecurringGrants(ctx)
	if err != nil {
		log.Printf("worker: error listing recurring grants: %v", err)
		return
	}

	now := time.Now().UTC().Truncate(time.Second)

	for _, p := range parents {
		dur, err := duration.Parse(*p.RecurrenceInterval)
		if err != nil {
			continue // skip invalid
		}
		
		// Find latest child to know base time
		latestEffective := p.EffectiveAt
		latestChild, err := st.GetLatestChildGrant(ctx, p.ID)
		if err == nil && latestChild != nil {
			latestEffective = latestChild.EffectiveAt
		}

		nextTime := shiftTime(latestEffective, dur)
		
		if !nextTime.After(now) {
			// Needs new child!
			child := &store.GrantRow{
				ID:            store.NewULID(),
				EntitlementID: p.EntitlementID,
				Amount:        p.Amount,
				Priority:      p.Priority,
				EffectiveAt:   nextTime,
				ParentGrantID: &p.ID,
				CreatedAt:     now,
			}
			
			// Optional limits: if p had an ExpiresAt, child expires = NextTime + (p.ExpiresAt - p.EffectiveAt)
			if p.ExpiresAt != nil {
				validity := p.ExpiresAt.Sub(p.EffectiveAt)
				exp := nextTime.Add(validity)
				child.ExpiresAt = &exp
			}

			// We rely on the UNIQUE INDEX (parent_grant_id, effective_at) for idempotency
			if err := st.CreateGrant(ctx, child); err != nil {
				log.Printf("worker: error creating child grant for parent %s: %v", p.ID, err)
			} else {
				log.Printf("worker: emitted recurring grant %s for parent %s", child.ID, p.ID)
			}
		}
	}
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
