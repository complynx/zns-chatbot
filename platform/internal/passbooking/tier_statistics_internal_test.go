package passbooking

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func TestTierStatisticsPreserveLegacySnapshot(t *testing.T) {
	t.Parallel()
	zero, nonzero, index := 0, 23, 99
	yes, no := true, false
	states := []string{pending, waitlist, assigned, paid, cancelled}
	roles := []passallocation.Role{passallocation.Leader, passallocation.Follower}
	prices := []*int{nil, &zero, &nonzero}
	skips := []*bool{nil, &yes, &no}
	bookings := map[string]*Booking{}
	for i := range len(states) * len(roles) * len(prices) * len(skips) * 2 {
		b := &Booking{
			State:       states[i%len(states)],
			Role:        roles[(i/len(states))%len(roles)],
			Price:       prices[(i/(len(states)*len(roles)))%len(prices)],
			SkipBalance: skips[(i/(len(states)*len(roles)*len(prices)))%len(skips)],
		}
		if i >= len(states)*len(roles)*len(prices)*len(skips) {
			b.TierIndex = &index
		}
		bookings[strconv.Itoa(i)] = b
	}
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, rule := range []passallocation.Rule{passallocation.Paired, passallocation.Distributed} {
		state := newSnapshot(
			event{
				id:   "dance",
				rule: rule,
				tiers: []passallocation.Tier{
					{Amount: 10, Price: 25, Start: now.Add(-time.Hour)},
					{Amount: 30, Price: 50, Start: now.Add(time.Hour)},
				},
			},
			bookings,
			now,
		)
		require.Equal(t, state.legacyTierStatus(), tierStatus(state.event, state.stats(), now))
	}
}

func TestTierUsageByteAccounting(t *testing.T) {
	t.Parallel()
	stats := newStatistics()
	size := 2
	for _, index := range []int{0, 0, 0, 9, 99, 999999, 0, 0, 0, 0, 0, 0, 0, 0} {
		b := Booking{State: paid, Role: passallocation.Leader, TierIndex: &index}
		size = nextUsageBytes(size, stats, &b)
		stats.observe(&b)
		encoded, err := json.Marshal(stats.total.Explicit)
		require.NoError(t, err)
		require.Equal(t, len(encoded), size)
	}
}

// These reference calculations retain the pre-streaming snapshot behavior.
func (s *snapshot) legacyStats() statistics {
	result := statistics{
		total: passallocation.Usage{Explicit: map[int]int{}},
		roles: map[passallocation.Role]passallocation.Usage{
			passallocation.Leader: {Explicit: map[int]int{}}, passallocation.Follower: {Explicit: map[int]int{}},
		},
	}
	for _, b := range s.bookings {
		if b.State == waitlist {
			addRole(&result.waiting, b.Role)
		}
		if b.State != assigned && b.State != paid {
			continue
		}
		if b.State == assigned {
			result.unpaid++
		}
		if (b.SkipBalance != nil && !*b.SkipBalance) || (b.SkipBalance == nil && b.Price != nil && *b.Price != 0) {
			addRole(&result.balance, b.Role)
		}
		result.total.Participants++
		role := result.roles[b.Role]
		role.Participants++
		if b.TierIndex != nil {
			result.total.Explicit[*b.TierIndex]++
			role.Explicit[*b.TierIndex]++
		}
		result.roles[b.Role] = role
	}
	return result
}

func (s *snapshot) legacyTierStatus() TierStatus {
	stats := s.legacyStats()
	result := TierStatus{
		Event:   s.event.id,
		Rule:    s.event.rule,
		Balance: stats.balance,
		Participants: passallocation.Counts{
			Leader:   stats.roles[passallocation.Leader].Participants,
			Follower: stats.roles[passallocation.Follower].Participants,
		},
		Current: s.legacyCurrentAdminTier(),
		Tiers:   s.event.tiers,
		Usage:   stats.total.Explicit,
	}
	result.FullBalance = result.Participants
	for _, b := range s.bookings {
		included := (b.SkipBalance != nil && !*b.SkipBalance) ||
			(b.SkipBalance == nil && (b.Price == nil || *b.Price != 0))
		if !included {
			continue
		}
		if b.State == assigned {
			addRole(&result.Assigned, b.Role)
		}
		if b.State == waitlist {
			addRole(&result.Waiting, b.Role)
		}
	}
	if result.Waiting.Leader+result.Waiting.Follower > 0 {
		result.Target = stats.balance.TargetRole(result.Waiting)
	}
	request := passallocation.TierRequest{Rule: s.event.rule, Usage: stats.total, Increment: 1, Now: s.now}
	if s.event.rule == passallocation.Distributed {
		result.Distributed = passallocation.DescribeTiers(s.event.tiers, request)
		if result.Waiting.Leader > 0 && result.Waiting.Follower > 0 {
			request.Couple = true
			request.Increment = 2
			result.Couple = passallocation.DescribeTiers(s.event.tiers, request).Current
		}
	} else {
		request.Usage = stats.roles[passallocation.Leader]
		result.Leader = passallocation.DescribeTiers(s.event.tiers, request)
		request.Usage = stats.roles[passallocation.Follower]
		result.Follower = passallocation.DescribeTiers(s.event.tiers, request)
	}
	return result
}
func (s *snapshot) legacyCurrentAdminTier() int {
	stats := s.legacyStats()
	usages := []passallocation.Usage{stats.total}
	if s.event.rule == passallocation.Paired {
		usages = []passallocation.Usage{stats.roles[passallocation.Leader], stats.roles[passallocation.Follower]}
	}
	current := 0
	for _, usage := range usages {
		index, ok := passallocation.PickTier(
			s.event.tiers,
			passallocation.TierRequest{Rule: s.event.rule, Usage: usage, Increment: 1, Now: s.now},
		)
		if ok {
			current = max(current, index+1)
		}
	}
	if current == 0 {
		return len(s.event.tiers)
	}
	return current
}
