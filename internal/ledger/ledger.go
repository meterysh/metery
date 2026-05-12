package ledger

import (
	"sort"
	"time"

	"github.com/sosodev/duration"
)

type Grant struct {
	ID           string
	Amount       int64
	Priority     int32
	EffectiveAt  time.Time
	ExpiresAt    *time.Time
	RolloverMax  *int64
	RolloverType string // "original", "remaining", or ""
}

type Entitlement struct {
	ID                  string
	UsagePeriodDuration *string
	UsagePeriodAnchor   *time.Time
	CreatedAt           time.Time
}

type Period struct {
	From time.Time
	To   time.Time
}

type BalanceResult struct {
	HasAccess bool
	Balance   int64
	Usage     int64
	Overage   int64
	Period    *Period
	LastReset *time.Time
}

// ActiveGrant tracks the state of a grant during balance calculation.
type ActiveGrant struct {
	Grant
	Remaining int64
}

// FetchUsageFn abstracts how usage is fetched for a specific time window.
type FetchUsageFn func(from, to time.Time) int64

// CalculateBalance computes the current ledger balance and state for an entitlement at time T.
func CalculateBalance(
	t time.Time,
	ent Entitlement,
	grants []Grant,
	fetchUsage FetchUsageFn,
) BalanceResult {
	if ent.UsagePeriodDuration == nil {
		// No reset periods: aggregate from the beginning of time
		return calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
	}

	// We have periods. We must compute from anchor (or CreatedAt) up to t.
	anchor := ent.CreatedAt
	if ent.UsagePeriodAnchor != nil {
		anchor = *ent.UsagePeriodAnchor
	}

	dur, err := duration.Parse(*ent.UsagePeriodDuration)
	if err != nil {
		// Fallback to stateless if parsing fails (in reality, should be validated at creation)
		return calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
	}

	// Find all periods from anchor to t
	periods := buildPeriods(anchor, dur, t)
	if len(periods) == 0 {
		return calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
	}

	var active []*ActiveGrant
	var usage int64
	var lastOverage int64

	for i, p := range periods {
		isCurrent := i == len(periods)-1

		// Add new grants that become effective in this period
		for _, g := range grants {
			if !g.EffectiveAt.Before(p.From) && g.EffectiveAt.Before(p.To) {
				active = append(active, &ActiveGrant{Grant: g, Remaining: g.Amount})
			}
			// Also include grants that were effective before the FIRST period we evaluate
			if i == 0 && g.EffectiveAt.Before(p.From) {
				active = append(active, &ActiveGrant{Grant: g, Remaining: g.Amount})
			}
		}

		// Prune expired grants
		filtered := make([]*ActiveGrant, 0, len(active))
		for _, ag := range active {
			if ag.ExpiresAt != nil && !ag.ExpiresAt.After(p.From) {
				continue // expired before or at the start of this period
			}
			filtered = append(filtered, ag)
		}
		active = filtered

		// Sort active grants by Priority (asc), EffectiveAt (asc)
		sort.Slice(active, func(i, j int) bool {
			if active[i].Priority != active[j].Priority {
				return active[i].Priority < active[j].Priority
			}
			return active[i].EffectiveAt.Before(active[j].EffectiveAt)
		})

		// Fetch usage for this period
		endOfFetch := p.To
		if isCurrent {
			endOfFetch = t
		}
		usage = fetchUsage(p.From, endOfFetch)

		// Deduct usage from grants
		remainingUsage := usage
		for _, ag := range active {
			if remainingUsage <= 0 {
				break
			}
			if ag.Remaining >= remainingUsage {
				ag.Remaining -= remainingUsage
				remainingUsage = 0
			} else {
				remainingUsage -= ag.Remaining
				ag.Remaining = 0
			}
		}

		lastOverage = remainingUsage

		// Rollover for the next period
		if !isCurrent {
			rolledOver := make([]*ActiveGrant, 0, len(active))
			for _, ag := range active {
				if ag.Remaining == 0 {
					continue // fully consumed
				}
				if ag.ExpiresAt != nil && !ag.ExpiresAt.After(p.To) {
					continue // expires exactly at or before period end
				}

				if ag.RolloverMax != nil {
					maxRollover := *ag.RolloverMax
					if ag.RolloverType == "original" {
						if ag.Amount < maxRollover {
							maxRollover = ag.Amount
						}
					}
					if ag.Remaining > maxRollover {
						ag.Remaining = maxRollover
					}
				}
				if ag.Remaining > 0 {
					rolledOver = append(rolledOver, ag)
				}
			}
			active = rolledOver
		}
	}

	// Calculate final balance from active grants
	var totalBalance int64
	for _, ag := range active {
		if ag.ExpiresAt == nil || ag.ExpiresAt.After(t) {
			totalBalance += ag.Remaining
		}
	}

	currentPeriod := periods[len(periods)-1]

	return BalanceResult{
		HasAccess: totalBalance > 0,
		Balance:   totalBalance,
		Usage:     usage,
		Overage:   lastOverage,
		Period:    &currentPeriod,
		LastReset: &currentPeriod.From,
	}
}

func calculateStatelessBalance(t time.Time, ent Entitlement, grants []Grant, usage int64) BalanceResult {
	var active []*ActiveGrant
	for _, g := range grants {
		if !g.EffectiveAt.After(t) && (g.ExpiresAt == nil || g.ExpiresAt.After(t)) {
			active = append(active, &ActiveGrant{Grant: g, Remaining: g.Amount})
		}
	}

	sort.Slice(active, func(i, j int) bool {
		if active[i].Priority != active[j].Priority {
			return active[i].Priority < active[j].Priority
		}
		return active[i].EffectiveAt.Before(active[j].EffectiveAt)
	})

	remainingUsage := usage
	for _, ag := range active {
		if remainingUsage <= 0 {
			break
		}
		if ag.Remaining >= remainingUsage {
			ag.Remaining -= remainingUsage
			remainingUsage = 0
		} else {
			remainingUsage -= ag.Remaining
			ag.Remaining = 0
		}
	}

	var totalBalance int64
	for _, ag := range active {
		totalBalance += ag.Remaining
	}

	return BalanceResult{
		HasAccess: totalBalance > 0,
		Balance:   totalBalance,
		Usage:     usage,
		Overage:   remainingUsage,
		Period:    nil,
		LastReset: nil,
	}
}

func buildPeriods(anchor time.Time, dur *duration.Duration, t time.Time) []Period {
	var periods []Period

	curr := anchor
	// Step backward if t < anchor
	for curr.After(t) {
		curr = shiftTime(curr, dur, -1)
	}

	// Step forward until we reach the period containing t
	for {
		next := shiftTime(curr, dur, 1)
		periods = append(periods, Period{From: curr, To: next})
		if !next.After(t) {
			curr = next
		} else {
			break
		}
	}
	return periods
}

func shiftTime(t time.Time, d *duration.Duration, mult int) time.Time {
	sign := 1
	if d.Negative {
		sign = -1
	}
	sign *= mult

	years := int(d.Years) * sign
	months := int(d.Months) * sign
	days := int(d.Weeks)*7*sign + int(d.Days)*sign

	shifted := t.AddDate(years, months, days)

	hours := time.Duration(d.Hours) * time.Hour
	minutes := time.Duration(d.Minutes) * time.Minute
	seconds := time.Duration(d.Seconds) * time.Second

	totalDuration := hours + minutes + seconds
	if sign < 0 {
		totalDuration = -totalDuration
	}

	return shifted.Add(totalDuration)
}
