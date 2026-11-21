package registrations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestQueueBalanceChangesAllowOnlyConfiguredRoleOrBalanceImprovement(t *testing.T) {
	t.Parallel()
	r := registrations.PassQueueRules{AutoAssign: true}
	require.False(t, r.Allows(0, 0, 0, 1))
	require.True(t, r.Allows(0, 0, 1, 1))
	require.False(t, r.Allows(1, 1, 0, 1))
	require.True(t, r.Allows(0, 4, 1, 0))
	require.False(t, r.Allows(0, 4, 0, 1))
	r.MaxFollowerPercent = 67
	require.True(t, r.Allows(1, 1, 0, 1))
	require.False(t, r.Allows(1, 2, 0, 1))
	require.False(t, r.Allows(1, 1, 1, 0))
}

func TestAutoAssignmentOrdinaryQuotesSkipPromoAndRespectDateBarrier(t *testing.T) {
	t.Parallel()
	now := pricingInstant()
	promo := priceTier("promo", 1, 80, now)
	promo.Promo = true
	next := priceTier("ordinary", 8, 180, now.Add(time.Hour))
	rules := pricingRules(registrations.PricingDistributed, promo, next)
	rules.AutoAssign = true
	roles := []string{registrations.Leader}
	_, err := registrations.ResolvePrices(rules, now, nil, roles)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	usage := []registrations.TierUsage{{Role: registrations.Follower, TierID: "promo", Count: 1}}
	prices, err := registrations.ResolvePrices(rules, now, usage, roles)
	require.NoError(t, err)
	require.Equal(t, "ordinary", prices[0].Tier.ID)
	rules.Pricing.Tiers[1].BlockedByDate = true
	_, err = registrations.ResolvePrices(rules, now, usage, roles)
	require.ErrorIs(t, err, registrations.ErrCapacity)
}

func TestManualPromoRemainsAvailableToSoloOwners(t *testing.T) {
	t.Parallel()
	now := pricingInstant()
	promo := priceTier("solo-promo", 4, 70, now)
	promo.Promo = true
	rules := pricingRules(registrations.PricingDistributed, promo)
	rules.SalesOpensAt = &now
	solo, err := registrations.ResolvePrices(rules, now, nil, []string{registrations.Leader})
	require.NoError(t, err)
	require.Equal(t, "solo-promo", solo[0].Tier.ID)
	pair := []string{registrations.Follower, registrations.Leader}
	_, err = registrations.ResolvePrices(rules, now, nil, pair)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	rules.SalesOpensAt = nil
	_, err = registrations.ResolveAssignmentPrices(rules, now, nil, pair, true)
	require.ErrorIs(t, err, registrations.ErrCapacity)
}

func TestQueuePairSplitsOrdinaryLotAtLastPlaceInRegistrationOrder(t *testing.T) {
	t.Parallel()
	now := pricingInstant()
	r := pricingRules(
		registrations.PricingDistributed,
		priceTier("early", 3, 80, now),
		priceTier("later", 4, 120, now.Add(time.Hour)),
	)
	r.AutoAssign = true
	q, err := registrations.ResolvePrices(
		r,
		now,
		[]registrations.TierUsage{{Role: registrations.Leader, TierID: "early", Count: 2}},
		[]string{registrations.Follower, registrations.Leader},
	)
	require.NoError(t, err)
	require.Equal(t, "early", q[0].Tier.ID)
	require.Equal(t, "later", q[1].Tier.ID)
	r.Pricing.Tiers[1].BlockedByDate = true
	_, err = registrations.ResolvePrices(
		r,
		now,
		[]registrations.TierUsage{{Role: registrations.Leader, TierID: "early", Count: 2}},
		[]string{registrations.Follower, registrations.Leader},
	)
	require.ErrorIs(t, err, registrations.ErrCapacity)
}
