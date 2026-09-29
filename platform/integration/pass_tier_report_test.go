package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestPassTierStatusBalanceAndRoleEligibility(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET assignment_rule='paired'; UPDATE core.pass_bookings SET state='assigned',assigned_at=now(),price=0,tier_index=0,skip_balance=true WHERE owner='alice'`,
	)
	require.NoError(t, err)
	status, err := service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, passallocation.Counts{}, status.Balance)
	assert.Equal(t, passallocation.Counts{Leader: 1}, status.FullBalance)
	assert.Equal(t, passallocation.Counts{}, status.Assigned)
	assert.Equal(t, passallocation.Follower, status.Target)
	require.NotNil(t, status.Leader.Current)
	require.NotNil(t, status.Follower.Current)
	assert.Equal(t, 1, status.Leader.Current.Used)
	assert.Zero(t, status.Follower.Current.Used)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_event_tiers SET starts_at=now()+interval '1 day',blocked_by_date=true`,
	)
	require.NoError(t, err)
	status, err = service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Nil(t, status.Leader.Current)
	require.NotNil(t, status.Leader.Next)
	assert.True(t, status.Leader.Next.Future)
	assert.True(t, status.Leader.Next.Tier.BlockedByDate)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_event_tiers`)
	require.NoError(t, err)
	status, err = service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Empty(t, status.Tiers)
	assert.Nil(t, status.Leader.Current)
}

func TestPassTierStatusDistributedCoupleGate(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	status, err := service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.NotNil(t, status.Distributed.Current)
	require.NotNil(t, status.Couple)
	assert.Equal(t, 1, status.Couple.Number)
	assert.False(t, status.Couple.Overflow)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET promo=true`)
	require.NoError(t, err)
	status, err = service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	require.NotNil(t, status.Distributed.Current)
	assert.Nil(t, status.Couple)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_event_tiers SET promo=false; UPDATE core.pass_bookings SET skip_balance=true WHERE owner='bob'`,
	)
	require.NoError(t, err)
	status, err = service.TierStatus(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Nil(t, status.Couple)
	assert.Equal(t, passallocation.Counts{Leader: 1}, status.Waiting)
	assert.Equal(t, passallocation.Leader, status.Target)
}
