package passallocation_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestTierReportUsesAllocatorEligibility(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tiers := []passallocation.Tier{
		{Amount: 4, Price: 100, Start: now.Add(-time.Hour)},
		{Amount: 4, Price: 200, Start: now.Add(time.Hour)},
	}
	request := passallocation.TierRequest{
		Rule:      passallocation.Distributed,
		Usage:     passallocation.Usage{Participants: 3},
		Increment: 2,
		Couple:    true,
		Now:       now,
	}
	report := passallocation.DescribeTiers(tiers, request)
	require.NotNil(t, report.Current)
	assert.Equal(t, 1, report.Current.Number)
	assert.True(t, report.Current.Overflow)
	assert.Equal(t, 1, report.Current.Left)
	assert.Equal(t, 3, report.Current.Used)
	assert.Zero(t, report.Current.Explicit)
	tiers[1].BlockedByDate = true
	report = passallocation.DescribeTiers(tiers, request)
	assert.Nil(t, report.Current)
	require.NotNil(t, report.Next)
	tiers[0].Promo = true
	report = passallocation.DescribeTiers(tiers, request)
	assert.Nil(t, report.Current)
}

func TestTierReportDateGateAndCarryover(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tiers := []passallocation.Tier{
		{Amount: 10, Start: now.Add(-2 * time.Hour)},
		{Amount: 10, Start: now.Add(-time.Hour)},
	}
	request := passallocation.TierRequest{
		Rule:      passallocation.Paired,
		Usage:     passallocation.Usage{Participants: 2},
		Increment: 1,
		Now:       now,
	}
	report := passallocation.DescribeTiers(tiers, request)
	require.NotNil(t, report.Current)
	assert.Equal(t, 2, report.Current.Number)
	assert.Equal(t, 8, report.Current.Left)
	assert.Equal(t, 5, report.Current.Capacity)
	tiers[0].Start = now.Add(time.Hour)
	tiers[0].BlockedByDate = true
	tiers[1].Start = now.Add(2 * time.Hour)
	report = passallocation.DescribeTiers(tiers, request)
	assert.Nil(t, report.Current)
	require.NotNil(t, report.Next)
	assert.True(t, report.Next.Future)
	assert.True(t, report.Next.Tier.BlockedByDate)
	assert.Equal(t, 2, report.Next.Participants)
	assert.Empty(t, passallocation.DescribeTiers(nil, request))
}
