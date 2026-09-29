package passallocation_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestBalance(t *testing.T) {
	t.Parallel()
	counts := passallocation.Counts{Leader: 10, Follower: 7}
	assert.False(t, counts.Balanced())
	assert.True(t, counts.Allows(passallocation.Counts{Follower: 1}, false))
	assert.False(t, counts.Allows(passallocation.Counts{Leader: 1}, false))
	assert.True(t, counts.Allows(passallocation.Counts{Leader: 1, Follower: 1}, false))
	assert.False(t, counts.Allows(passallocation.Counts{Leader: 1, Follower: 1}, true))
	assert.True(t, (passallocation.Counts{Leader: 52, Follower: 48}).Balanced())
	assert.False(t, (passallocation.Counts{Leader: 53, Follower: 47}).Balanced())
	assert.True(t, (passallocation.Counts{Leader: 1}).Balanced())
	assert.False(t, (passallocation.Counts{Leader: 1}).Allows(passallocation.Counts{Leader: 1}, false))
	assert.Equal(t, passallocation.Follower, counts.TargetRole(passallocation.Counts{Leader: 20}))
	assert.Equal(t, passallocation.Leader, (passallocation.Counts{}).TargetRole(passallocation.Counts{}))
	assert.Equal(t, passallocation.Follower, (passallocation.Counts{}).TargetRole(passallocation.Counts{Follower: 1}))
}

func TestConcurrency(t *testing.T) {
	t.Parallel()
	assert.True(t, passallocation.HasConcurrencyCapacity(8, 2, false))
	assert.False(t, passallocation.HasConcurrencyCapacity(9, 2, false))
	assert.True(t, passallocation.HasConcurrencyCapacity(9, 1, false))
	assert.False(t, passallocation.HasConcurrencyCapacity(10, 1, false))
	assert.True(t, passallocation.HasConcurrencyCapacity(10, 2, true))
}

func TestTierEligibility(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	tests := []struct {
		name    string
		tiers   []passallocation.Tier
		request passallocation.TierRequest
		want    int
		ok      bool
	}{
		{name: "no tiers", request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 1}},
		{
			name:    "first future tier not effective",
			tiers:   []passallocation.Tier{{Amount: 10, Start: future}},
			request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 1},
		},
		{
			name:  "odd paired capacity floors",
			tiers: []passallocation.Tier{{Amount: 5, Start: past}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Paired,
				Increment: 1,
				Usage:     passallocation.Usage{Participants: 2},
			},
		},
		{
			name:  "historical participants fill earlier tier",
			tiers: []passallocation.Tier{{Amount: 4, Start: past}, {Amount: 4, Start: future}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Distributed,
				Increment: 1,
				Usage:     passallocation.Usage{Participants: 4},
			},
			want: 1,
			ok:   true,
		},
		{
			name:  "explicit usage cannot be undercounted",
			tiers: []passallocation.Tier{{Amount: 4, Start: past}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Distributed,
				Increment: 1,
				Usage:     passallocation.Usage{Participants: 1, Explicit: map[int]int{0: 4}},
			},
		},
		{
			name:    "time floor skips unsold tier",
			tiers:   []passallocation.Tier{{Amount: 4, Start: past}, {Amount: 4, Start: now}},
			request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 1},
			want:    1,
			ok:      true,
		},
		{
			name:  "distributed couple overflow",
			tiers: []passallocation.Tier{{Amount: 5, Start: past}, {Amount: 5, Start: future}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Distributed,
				Increment: 2,
				Couple:    true,
				Usage:     passallocation.Usage{Participants: 4},
			},
			ok: true,
		},
		{
			name:  "last tier cannot overflow",
			tiers: []passallocation.Tier{{Amount: 5, Start: past}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Distributed,
				Increment: 2,
				Couple:    true,
				Usage:     passallocation.Usage{Participants: 4},
			},
		},
		{
			name:  "blocked next tier cannot unblock couple",
			tiers: []passallocation.Tier{{Amount: 5, Start: past}, {Amount: 5, Start: future, BlockedByDate: true}},
			request: passallocation.TierRequest{
				Rule:      passallocation.Distributed,
				Increment: 2,
				Couple:    true,
				Usage:     passallocation.Usage{Participants: 4},
			},
		},
		{
			name: "promo blocks couple scan before skip",
			tiers: []passallocation.Tier{
				{Start: past, Promo: true},
				{Amount: 1, Start: future, Promo: true, BlockedByDate: true},
				{Amount: 5, Start: future},
			},
			request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 2, Couple: true},
		},
		{
			name: "admin ignores promo date barrier",
			tiers: []passallocation.Tier{
				{Start: past, Promo: true},
				{Amount: 1, Start: future, Promo: true, BlockedByDate: true},
				{Amount: 5, Start: future},
			},
			request: passallocation.TierRequest{
				Rule:             passallocation.Distributed,
				Increment:        2,
				Couple:           true,
				IgnoreDateBlocks: true,
			},
			want: 2,
			ok:   true,
		},
		{
			name:    "solo accepts promo",
			tiers:   []passallocation.Tier{{Amount: 5, Start: past, Promo: true}},
			request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 1},
			ok:      true,
		},
		{
			name:    "couple skips promo",
			tiers:   []passallocation.Tier{{Amount: 1, Start: past, Promo: true}, {Amount: 5, Start: future}},
			request: passallocation.TierRequest{Rule: passallocation.Distributed, Increment: 2, Couple: true},
			want:    1,
			ok:      true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := test.request
			request.Now = now
			index, ok := passallocation.PickTier(test.tiers, request)
			require.Equal(t, test.ok, ok)
			if ok {
				assert.Equal(t, test.want, index)
			}
		})
	}
}

func TestPairedCouplePricesCanDiffer(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 25, 12, 0, 0, 0, time.UTC)
	tiers := []passallocation.Tier{
		{Amount: 4, Price: 100, Start: now},
		{Amount: 4, Price: 200, Start: now.Add(time.Hour)},
	}
	leader, ok := passallocation.PickTier(
		tiers,
		passallocation.TierRequest{
			Rule:      passallocation.Paired,
			Usage:     passallocation.Usage{Participants: 2},
			Increment: 1,
			Couple:    true,
			Now:       now,
		},
	)
	require.True(t, ok)
	follower, ok := passallocation.PickTier(
		tiers,
		passallocation.TierRequest{
			Rule:      passallocation.Paired,
			Usage:     passallocation.Usage{Participants: 1},
			Increment: 1,
			Couple:    true,
			Now:       now,
		},
	)
	require.True(t, ok)
	assert.Equal(t, 200, tiers[leader].Price)
	assert.Equal(t, 100, tiers[follower].Price)
}
