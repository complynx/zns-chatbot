package registrations_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

func pricingInstant() time.Time {
	return time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
}

func pricingRules(mode string, tiers ...registrations.Tier) registrations.Rules {
	return registrations.Rules{
		LeaderCapacity: 100, FollowerCapacity: 100, PriceMinor: 999,
		Currency: "EUR", CurrencyScale: 2, ClosesAt: pricingInstant().Add(24 * time.Hour),
		PaymentWindowSeconds: 3600, Pricing: &registrations.TierPolicy{Mode: mode, Tiers: tiers},
	}
}

func priceTier(id string, amount int32, price int64, starts time.Time) registrations.Tier {
	return registrations.Tier{ID: id, Amount: amount, PriceMinor: price, StartsAt: starts}
}

func TestPairedPricingResolvesIndependentRolePricesInOwnerOrder(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingPaired,
		priceTier("early", 5, 1000, pricingInstant().Add(-time.Hour)),
		priceTier("regular", 6, 2000, pricingInstant().Add(time.Hour)))
	quotes, err := registrations.ResolvePrices(rules, pricingInstant(),
		[]registrations.TierUsage{{Role: registrations.Leader, TierID: "early", Count: 2}},
		[]string{registrations.Follower, registrations.Leader})
	require.NoError(t, err)
	require.Equal(t, []registrations.PriceQuote{
		{PriceMinor: 1000, Tier: &registrations.TierSnapshot{ID: "early", Number: 1}},
		{PriceMinor: 2000, Tier: &registrations.TierSnapshot{ID: "regular", Number: 2}},
	}, quotes)
}

func TestDistributedPairOvershootRequiresSuccessorBeforeDateBarrier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		successor    registrations.Tier
		wantCapacity bool
	}{
		{"positive future successor", priceTier("next", 4, 2000, pricingInstant().Add(time.Hour)), false},
		{"no successor", registrations.Tier{}, true},
		{"zero capacity successor", priceTier("next", 0, 2000, pricingInstant().Add(time.Hour)), true},
		{
			"promo successor",
			registrations.Tier{
				ID: "next", Amount: 4, PriceMinor: 2000, StartsAt: pricingInstant().Add(time.Hour), Promo: true,
			},
			true,
		},
		{
			"blocked successor",
			registrations.Tier{
				ID: "next", Amount: 4, PriceMinor: 2000, StartsAt: pricingInstant().Add(time.Hour), BlockedByDate: true,
			},
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rules := pricingRules(
				registrations.PricingDistributed,
				priceTier("early", 2, 1000, pricingInstant().Add(-time.Hour)),
			)
			if tc.successor.ID != "" {
				rules.Pricing.Tiers = append(rules.Pricing.Tiers, tc.successor)
			}
			quotes, err := registrations.ResolvePrices(rules, pricingInstant(),
				[]registrations.TierUsage{{Role: registrations.Leader, TierID: "early", Count: 1}},
				[]string{registrations.Leader, registrations.Follower})
			if tc.wantCapacity {
				require.ErrorIs(t, err, registrations.ErrCapacity)
				require.Nil(t, quotes)
				return
			}
			require.NoError(t, err)
			require.Len(t, quotes, 2)
			require.Equal(t, quotes[0], quotes[1])
			require.Equal(t, int64(1000), quotes[0].PriceMinor)
		})
	}
}

func TestPricingUsesLegacyAndRemovedTierPassesForImpliedUsage(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed,
		priceTier("early", 2, 1000, pricingInstant().Add(-time.Hour)),
		priceTier("next", 2, 2000, pricingInstant().Add(time.Hour)))
	for _, historicalTier := range []string{"", "removed"} {
		quotes, err := registrations.ResolvePrices(
			rules,
			pricingInstant(),
			[]registrations.TierUsage{
				{Role: registrations.Leader, TierID: historicalTier, Count: 2},
			},
			[]string{registrations.Follower},
		)
		require.NoError(t, err)
		require.Equal(t, "next", quotes[0].Tier.ID)
	}
}

func TestExplicitOvershootCountsAgainstLaterCapacityWithoutClamping(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed,
		priceTier("early", 2, 1000, pricingInstant().Add(-time.Hour)),
		priceTier("next", 2, 2000, pricingInstant().Add(time.Hour)))
	// The first pair exceeded early by one. That pass remains part of the
	// participant total, leaving only one place for a solo in the next tier.
	usage := []registrations.TierUsage{
		{Role: registrations.Leader, TierID: "early", Count: 2},
		{Role: registrations.Follower, TierID: "early", Count: 1},
	}
	quotes, err := registrations.ResolvePrices(rules, pricingInstant(), usage, []string{registrations.Follower})
	require.NoError(t, err)
	require.Equal(t, "next", quotes[0].Tier.ID)
	quotes, err = registrations.ResolvePrices(
		rules,
		pricingInstant(),
		usage,
		[]string{registrations.Leader, registrations.Follower},
	)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	require.Nil(t, quotes)
	// Explicit use survives a configured amount reduction and remains full.
	rules.Pricing.Tiers[0].Amount = 0
	quotes, err = registrations.ResolvePrices(rules, pricingInstant(), usage, []string{registrations.Leader})
	require.ErrorIs(t, err, registrations.ErrCapacity)
	require.Nil(t, quotes)
}

func TestPricingDateFloorRetainsConfiguredOrderAndDoesNotCarryUnsoldCapacity(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed,
		priceTier("old", 100, 1000, pricingInstant().Add(-time.Hour)),
		priceTier("current", 1, 2000, pricingInstant()))
	quotes, err := registrations.ResolvePrices(rules, pricingInstant(), nil, []string{registrations.Leader})
	require.NoError(t, err)
	require.Equal(t, "current", quotes[0].Tier.ID)
	quotes, err = registrations.ResolvePrices(
		rules,
		pricingInstant(),
		[]registrations.TierUsage{
			{Role: registrations.Leader, TierID: "current", Count: 1},
		},
		[]string{registrations.Follower},
	)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	require.Nil(t, quotes)
	// A later configured tier wins even when its start precedes an earlier one.
	rules.Pricing.Tiers[1].StartsAt = pricingInstant().Add(-2 * time.Hour)
	quotes, err = registrations.ResolvePrices(rules, pricingInstant(), nil, []string{registrations.Leader})
	require.NoError(t, err)
	require.Equal(t, "current", quotes[0].Tier.ID)
}

func TestDistributedPairOverflowConsumesSuccessorAfterUnsoldTierExpires(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		capacity int32
	}{
		{"successor filled by overflow", 1},
		{"one successor place remains", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rules := pricingRules(registrations.PricingDistributed,
				priceTier("old", 100, 1000, pricingInstant().Add(-time.Hour)),
				priceTier("current", 1, 2000, pricingInstant()),
				priceTier("next", tc.capacity, 3000, pricingInstant().Add(time.Hour)))
			roles := []string{registrations.Leader, registrations.Follower}
			quotes, err := registrations.ResolvePrices(rules, pricingInstant(), nil, roles)
			require.NoError(t, err)
			require.Len(t, quotes, 2)
			var usage []registrations.TierUsage
			for i, quote := range quotes {
				require.Equal(t, "current", quote.Tier.ID)
				usage = append(usage, registrations.TierUsage{Role: roles[i], TierID: quote.Tier.ID, Count: 1})
			}
			if tc.capacity == 2 {
				quotes, err = registrations.ResolvePrices(
					rules, pricingInstant(), usage, []string{registrations.Leader},
				)
				require.NoError(t, err)
				require.Equal(t, "next", quotes[0].Tier.ID)
				usage = append(usage, registrations.TierUsage{Role: registrations.Leader, TierID: "next", Count: 1})
			}
			quotes, err = registrations.ResolvePrices(rules, pricingInstant(), usage, []string{registrations.Follower})
			require.ErrorIs(t, err, registrations.ErrCapacity)
			require.Nil(t, quotes)
		})
	}
}

func TestPricingWaitsForFirstStartAndOpensAtExactInstant(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed,
		priceTier("first", 2, 1000, pricingInstant().Add(time.Hour)))
	_, err := registrations.ResolvePrices(rules, pricingInstant(), nil, []string{registrations.Leader})
	require.ErrorIs(t, err, registrations.ErrCapacity)
	quotes, err := registrations.ResolvePrices(
		rules,
		pricingInstant().Add(time.Hour),
		nil,
		[]string{registrations.Leader},
	)
	require.NoError(t, err)
	require.Equal(t, "first", quotes[0].Tier.ID)
}

func TestPricingPromoSoloAndPairDateBarriers(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed,
		registrations.Tier{ID: "promo", Amount: 2, PriceMinor: 500, StartsAt: pricingInstant(), Promo: true},
		priceTier("regular", 4, 1000, pricingInstant().Add(time.Hour)))
	quotes, err := registrations.ResolvePrices(rules, pricingInstant(), nil, []string{registrations.Leader})
	require.NoError(t, err)
	require.Equal(t, "promo", quotes[0].Tier.ID)
	// Promo is skipped for pairs, but its capacity does not disappear: the
	// future next tier needs its own start or sufficient assigned participants.
	_, err = registrations.ResolvePrices(
		rules, pricingInstant(), nil, []string{registrations.Leader, registrations.Follower},
	)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	rules.Pricing.Tiers[1].StartsAt = pricingInstant()
	quotes, err = registrations.ResolvePrices(
		rules,
		pricingInstant(),
		nil,
		[]string{registrations.Leader, registrations.Follower},
	)
	require.NoError(t, err)
	require.Equal(t, "regular", quotes[0].Tier.ID)
	// A blocked zero-capacity promo stops scanning before either skip rule.
	rules.Pricing.Tiers = []registrations.Tier{
		priceTier("first", 0, 1000, pricingInstant()),
		{
			ID:            "barrier",
			Amount:        0,
			PriceMinor:    1000,
			StartsAt:      pricingInstant().Add(time.Hour),
			Promo:         true,
			BlockedByDate: true,
		},
		priceTier("later", 4, 2000, pricingInstant().Add(2*time.Hour)),
	}
	_, err = registrations.ResolvePrices(
		rules, pricingInstant(), nil, []string{registrations.Leader, registrations.Follower},
	)
	require.ErrorIs(t, err, registrations.ErrCapacity)
}

func TestPricingFlatCompatibilityAndInputValidation(t *testing.T) {
	t.Parallel()
	rules := pricingRules(registrations.PricingDistributed, priceTier("early", 2, 1000, pricingInstant()))
	rules.Pricing = nil
	rules.PriceMinor = 0
	quotes, err := registrations.ResolvePrices(
		rules,
		pricingInstant(),
		nil,
		[]string{registrations.Leader, registrations.Follower},
	)
	require.NoError(t, err)
	require.Equal(t, []registrations.PriceQuote{{}, {}}, quotes)
	for _, roles := range [][]string{
		nil, {"invalid"}, {registrations.Leader, registrations.Leader},
		{registrations.Leader, registrations.Follower, registrations.Leader},
	} {
		_, err = registrations.ResolvePrices(rules, pricingInstant(), nil, roles)
		require.ErrorIs(t, err, registrations.ErrInvalid)
	}
	rules.Pricing = &registrations.TierPolicy{
		Mode:  registrations.PricingDistributed,
		Tiers: []registrations.Tier{priceTier("early", 2, 1000, pricingInstant())},
	}
	for _, usage := range [][]registrations.TierUsage{
		{{Role: "invalid", Count: 1}}, {{Role: registrations.Leader, Count: -1}},
		{{Role: registrations.Leader, Count: math.MaxInt64}},
		{{Role: registrations.Leader, Count: math.MaxInt64 - 2}, {Role: registrations.Follower, Count: 1}},
	} {
		_, err = registrations.ResolvePrices(rules, pricingInstant(), usage, []string{registrations.Leader})
		require.ErrorIs(t, err, registrations.ErrInvalid)
	}
}

func TestTierPolicyRejectsInvalidConfigurationOnValuesAndJSONReads(t *testing.T) {
	t.Parallel()
	valid := priceTier("early", 2, 1000, pricingInstant())
	for _, change := range []func(*registrations.TierPolicy){
		func(p *registrations.TierPolicy) { p.Mode = "unknown" },
		func(p *registrations.TierPolicy) { p.Tiers = nil },
		func(p *registrations.TierPolicy) { p.Tiers = make([]registrations.Tier, 101) },
		func(p *registrations.TierPolicy) { p.Tiers = append(p.Tiers, p.Tiers[0]) },
		func(p *registrations.TierPolicy) { p.Tiers[0].ID = "Invalid id" },
		func(p *registrations.TierPolicy) { p.Tiers[0].Amount = -1 },
		func(p *registrations.TierPolicy) { p.Tiers[0].PriceMinor = 0 },
		func(p *registrations.TierPolicy) { p.Tiers[0].PriceMinor = 1 << 53 },
		func(p *registrations.TierPolicy) { p.Tiers[0].StartsAt = time.Time{} },
		func(p *registrations.TierPolicy) { p.Tiers[0].StartsAt = pricingInstant().Add(time.Nanosecond) },
	} {
		rules := pricingRules(registrations.PricingPaired, valid)
		change(rules.Pricing)
		_, err := rules.Normalize()
		require.ErrorIs(t, err, registrations.ErrInvalid)
		data, err := json.Marshal(rules)
		require.NoError(t, err)
		var decoded registrations.Rules
		require.ErrorIs(t, json.Unmarshal(data, &decoded), registrations.ErrInvalid)
	}
	for _, document := range []string{
		`{"id":"x","price_minor":1,"starts_at":"2026-10-14T12:00:00Z"}`,
		`{"id":"x","amount":0,"price_minor":1,"starts_at":"2026-10-14T12:00:00"}`,
		`{"id":"x","amount":0,"price_minor":1,"starts_at":null}`,
		`{"id":"x","amount":0,"price_minor":1,"starts_at":"2026-10-14T12:00:00Z","unknown":1}`,
	} {
		var tier registrations.Tier
		require.ErrorIs(t, json.Unmarshal([]byte(document), &tier), registrations.ErrInvalid)
	}
}
