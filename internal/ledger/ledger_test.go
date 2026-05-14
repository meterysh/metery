package ledger

import (
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestCalculateBalance_Stateless(t *testing.T) {
	ent := Entitlement{
		ID:        "ent_1",
		CreatedAt: mustParseTime("2026-01-01T00:00:00Z"),
	}

	grants := []Grant{
		{
			ID:          "g1",
			Amount:      1000,
			Priority:    100,
			EffectiveAt: mustParseTime("2026-01-01T00:00:00Z"),
		},
		{
			ID:          "g2",
			Amount:      500,
			Priority:    10, // higher priority (lower number)
			EffectiveAt: mustParseTime("2026-01-05T00:00:00Z"),
		},
	}

	fetchUsage := func(from, to time.Time) int64 {
		return 300
	}

	evalTime := mustParseTime("2026-01-10T00:00:00Z")
	res, _ := CalculateBalance(evalTime, ent, grants, fetchUsage, nil)

	// total grants = 1500. usage = 300.
	// g2 (500) burns first because of priority 10.
	// g2 remaining = 200, g1 remaining = 1000. total balance = 1200.
	if res.Balance != 1200 {
		t.Fatalf("expected balance 1200, got %d", res.Balance)
	}
	if res.Usage != 300 {
		t.Fatalf("expected usage 300, got %d", res.Usage)
	}
	if res.Overage != 0 {
		t.Fatalf("expected overage 0, got %d", res.Overage)
	}
}

func TestCalculateBalance_WithPeriodsAndRollover(t *testing.T) {
	ent := Entitlement{
		ID:                  "ent_1",
		UsagePeriodDuration: ptr("P1M"),
		UsagePeriodAnchor:   ptr(mustParseTime("2026-01-01T00:00:00Z")),
		CreatedAt:           mustParseTime("2026-01-01T00:00:00Z"),
	}

	grants := []Grant{
		{
			ID:           "g1",
			Amount:       100,
			Priority:     100,
			EffectiveAt:  mustParseTime("2026-01-01T00:00:00Z"),
			RolloverMax:  ptr(int64(50)),
			RolloverType: "original",
		},
	}

	// Jan usage: 30
	// Feb usage: 10
	fetchUsage := func(from, to time.Time) int64 {
		if from.Month() == time.January {
			return 30
		}
		if from.Month() == time.February {
			return 10
		}
		return 0
	}

	evalTime := mustParseTime("2026-02-15T00:00:00Z")
	res, _ := CalculateBalance(evalTime, ent, grants, fetchUsage, nil)

	// Explanation:
	// Jan 1: grant = 100
	// Jan usage = 30. Remaining at end of Jan = 70.
	// Rollover to Feb: max 50. So start of Feb remaining = 50.
	// Feb usage = 10.
	// Feb mid-month remaining = 40.
	if res.Balance != 40 {
		t.Fatalf("expected balance 40, got %d", res.Balance)
	}
	if res.Usage != 10 {
		t.Fatalf("expected usage 10, got %d", res.Usage)
	}
}
