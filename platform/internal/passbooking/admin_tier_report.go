package passbooking

import (
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func tierStatus(e event, stats statistics, now time.Time) TierStatus {
	result := TierStatus{
		Event:   e.id,
		Rule:    e.rule,
		Balance: stats.balance,
		Participants: passallocation.Counts{
			Leader:   stats.roles[passallocation.Leader].Participants,
			Follower: stats.roles[passallocation.Follower].Participants,
		},
		Current: currentAdminTier(e, stats, now),
		Tiers:   e.tiers,
		Usage:   stats.total.Explicit,
	}
	result.FullBalance = result.Participants
	result.Assigned = stats.assignedIncluded
	result.Waiting = stats.waitingIncluded
	if result.Waiting.Leader+result.Waiting.Follower > 0 {
		result.Target = stats.balance.TargetRole(result.Waiting)
	}
	request := passallocation.TierRequest{Rule: e.rule, Usage: stats.total, Increment: 1, Now: now}
	if e.rule == passallocation.Distributed {
		result.Distributed = passallocation.DescribeTiers(e.tiers, request)
		if result.Waiting.Leader > 0 && result.Waiting.Follower > 0 {
			request.Couple = true
			request.Increment = 2
			result.Couple = passallocation.DescribeTiers(e.tiers, request).Current
		}
	} else {
		request.Usage = stats.roles[passallocation.Leader]
		result.Leader = passallocation.DescribeTiers(e.tiers, request)
		request.Usage = stats.roles[passallocation.Follower]
		result.Follower = passallocation.DescribeTiers(e.tiers, request)
	}
	return result
}
