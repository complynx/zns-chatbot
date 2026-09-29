package passbooking

import "github.com/complynx/zns-chatbot/platform/internal/passallocation"

func (s *snapshot) tierStatus() TierStatus {
	stats := s.stats()
	result := TierStatus{
		Event:   s.event.id,
		Rule:    s.event.rule,
		Balance: stats.balance,
		Participants: passallocation.Counts{
			Leader:   stats.roles[passallocation.Leader].Participants,
			Follower: stats.roles[passallocation.Follower].Participants,
		},
		Current: s.currentAdminTier(),
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
