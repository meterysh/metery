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

// GrantState is one entry in a Snapshot's per-grant remaining amounts.
type GrantState struct {
	GrantID   string `json:"grant_id"`
	Remaining int64  `json:"remaining"`
}

// Snapshot is the ledger state at a period boundary. Used to seed balance
// computation so only the current period's events need to be replayed.
type Snapshot struct {
	AsOf          time.Time    `json:"as_of"`
	Balance       int64        `json:"balance"`
	PerGrantState []GrantState `json:"per_grant_state"`
}

// FetchUsageFn abstracts how usage is fetched for a specific time window.
type FetchUsageFn func(from, to time.Time) int64

// CalculateBalance computes the current ledger balance and state for an
// entitlement at time T. seed, if non-nil, must be at a period boundary;
// periods before it are skipped and per-grant remaining is seeded from it.
// Returns any new snapshots captured at period boundaries during computation.
func CalculateBalance(
	t time.Time,
	ent Entitlement,
	grants []Grant,
	fetchUsage FetchUsageFn,
	seed *Snapshot,
) (BalanceResult, []Snapshot) {
	if ent.UsagePeriodDuration == nil {
		res := calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
		return res, nil
	}

	anchor := ent.CreatedAt
	if ent.UsagePeriodAnchor != nil {
		anchor = *ent.UsagePeriodAnchor
	}

	dur, err := duration.Parse(*ent.UsagePeriodDuration)
	if err != nil {
		res := calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
		return res, nil
	}

	periods := buildPeriods(anchor, dur, t)
	if len(periods) == 0 {
		res := calculateStatelessBalance(t, ent, grants, fetchUsage(ent.CreatedAt, t))
		return res, nil
	}

	// Find starting period and seed active grants from snapshot if provided.
	startIdx := 0
	var active []*ActiveGrant
	seededGrantIDs := map[string]struct{}{}

	if seed != nil {
		found := false
		for i, p := range periods {
			if p.From.Equal(seed.AsOf) {
				startIdx = i
				found = true
				break
			}
		}
		if found {
			grantMap := make(map[string]Grant, len(grants))
			for _, g := range grants {
				grantMap[g.ID] = g
			}
			for _, gs := range seed.PerGrantState {
				if g, ok := grantMap[gs.GrantID]; ok {
					active = append(active, &ActiveGrant{Grant: g, Remaining: gs.Remaining})
					seededGrantIDs[gs.GrantID] = struct{}{}
				}
			}
		}
	}

	var newSnapshots []Snapshot
	var usage int64
	var lastOverage int64

	for i := startIdx; i < len(periods); i++ {
		p := periods[i]
		isCurrent := i == len(periods)-1

		// Add grants effective in this period, or before the first evaluated period.
		for _, g := range grants {
			if _, seeded := seededGrantIDs[g.ID]; seeded {
				continue
			}
			inPeriod := !g.EffectiveAt.Before(p.From) && g.EffectiveAt.Before(p.To)
			preFirst := i == startIdx && g.EffectiveAt.Before(p.From)
			if inPeriod || preFirst {
				active = append(active, &ActiveGrant{Grant: g, Remaining: g.Amount})
				seededGrantIDs[g.ID] = struct{}{}
			}
		}

		// Prune expired grants.
		filtered := make([]*ActiveGrant, 0, len(active))
		for _, ag := range active {
			if ag.ExpiresAt != nil && !ag.ExpiresAt.After(p.From) {
				continue
			}
			filtered = append(filtered, ag)
		}
		active = filtered

		sort.Slice(active, func(i, j int) bool {
			if active[i].Priority != active[j].Priority {
				return active[i].Priority < active[j].Priority
			}
			return active[i].EffectiveAt.Before(active[j].EffectiveAt)
		})

		endOfFetch := p.To
		if isCurrent {
			endOfFetch = t
		}
		usage = fetchUsage(p.From, endOfFetch)

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

		if !isCurrent {
			rolledOver := make([]*ActiveGrant, 0, len(active))
			for _, ag := range active {
				if ag.Remaining == 0 {
					continue
				}
				if ag.ExpiresAt != nil && !ag.ExpiresAt.After(p.To) {
					continue
				}
				if ag.RolloverMax != nil {
					maxRollover := *ag.RolloverMax
					if ag.RolloverType == "original" && ag.Amount < maxRollover {
						maxRollover = ag.Amount
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

			// Capture snapshot at the period boundary (after rollover).
			snap := Snapshot{AsOf: p.To}
			for _, ag := range active {
				snap.PerGrantState = append(snap.PerGrantState, GrantState{
					GrantID:   ag.ID,
					Remaining: ag.Remaining,
				})
				snap.Balance += ag.Remaining
			}
			newSnapshots = append(newSnapshots, snap)
		}
	}

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
	}, newSnapshots
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
