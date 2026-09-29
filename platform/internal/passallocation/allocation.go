// Package passallocation contains pure pass eligibility decisions from the Python allocator.
// Callers provide a consistent event snapshot and commit assignments separately.
package passallocation

import "time"

type Rule string

const (
	Paired      Rule = "paired"
	Distributed Rule = "distributed"
)

type Role string

const (
	Leader   Role = "leader"
	Follower Role = "follower"
)

const balancePercent = 52
const concurrentLimit = 10
const rolesPerPair = 2

// Counts contains assigned plus paid participants included in balance calculations.
type Counts struct{ Leader, Follower int }

func (c Counts) Balanced() bool {
	total := c.Leader + c.Follower
	return total <= 1 || max(c.Leader, c.Follower)*100 <= balancePercent*total
}

// Allows permits non-worsening imbalance, except strict double-solo assignments.
// Counts and additions must be nonnegative participant counts.
func (c Counts) Allows(add Counts, strict bool) bool {
	next := Counts{Leader: c.Leader + add.Leader, Follower: c.Follower + add.Follower}
	if c.Balanced() {
		return next.Balanced()
	}
	if strict {
		return false
	}
	if difference(next) > difference(c) {
		return false
	}
	return max(next.Leader, next.Follower)*(c.Leader+c.Follower) <=
		max(c.Leader, c.Follower)*(next.Leader+next.Follower)
}

func difference(c Counts) int { return max(c.Leader, c.Follower) - min(c.Leader, c.Follower) }

// TargetRole chooses minority when imbalanced, otherwise the larger waitlist.
func (c Counts) TargetRole(waitlist Counts) Role {
	if !c.Balanced() {
		if c.Leader > c.Follower {
			return Follower
		}
		return Leader
	}
	if waitlist.Leader > waitlist.Follower || (waitlist.Leader == waitlist.Follower && c.Leader <= c.Follower) {
		return Leader
	}
	return Follower
}

// HasConcurrencyCapacity counts all assigned-unpaid records, including free/balance exclusions.
func HasConcurrencyCapacity(assigned, increment int, disabled bool) bool {
	return disabled || assigned+increment <= concurrentLimit
}

type Tier struct {
	Amount        int
	Price         int
	Start         time.Time
	Promo         bool
	BlockedByDate bool
}

// Usage includes all assigned/paid participants, including those without tier markers.
// Explicit maps zero-based tier indexes to participant counts.
type Usage struct {
	Participants int
	Explicit     map[int]int
}

// TierRequest uses role-specific usage for Paired and total usage for Distributed.
// Paired callers invoke PickTier independently for each couple member with increment one.
// Distributed couples use increment two and Couple=true. Solos use increment one.
type TierRequest struct {
	Rule             Rule
	Usage            Usage
	Increment        int
	Couple           bool
	IgnoreDateBlocks bool
	Now              time.Time
}

func capacity(tier Tier, rule Rule) int {
	if rule == Paired {
		return tier.Amount / rolesPerPair
	}
	return tier.Amount
}

func priorCapacity(tiers []Tier, index int, rule Rule) int {
	total := 0
	for _, tier := range tiers[:index] {
		total += capacity(tier, rule)
	}
	return total
}

func (r TierRequest) used(tiers []Tier, index int) int {
	implied := max(0, min(r.Usage.Participants-priorCapacity(tiers, index, r.Rule), capacity(tiers[index], r.Rule)))
	return max(r.Usage.Explicit[index], implied)
}

func (r TierRequest) effective(tiers []Tier, index int) bool {
	if !tiers[index].Start.After(r.Now) {
		return true
	}
	if index == 0 {
		return false
	}
	if priorCapacity(tiers, index, r.Rule) < r.Usage.Participants+r.Increment {
		return true
	}
	for previous := index - 1; previous >= 0; previous-- {
		limit := capacity(tiers[previous], r.Rule)
		if limit > 0 {
			return r.used(tiers, previous) >= limit
		}
	}
	return false
}

func (r TierRequest) blocked(tier Tier) bool {
	return !r.IgnoreDateBlocks && tier.BlockedByDate && tier.Start.After(r.Now)
}

func (r TierRequest) following(tiers []Tier) bool {
	for _, tier := range tiers {
		if r.blocked(tier) {
			return false
		}
		if (!r.Couple || !tier.Promo) && capacity(tier, r.Rule) > 0 {
			return true
		}
	}
	return false
}

// PickTier returns a zero-based tier index. False means no eligible tier.
// Input tiers retain configured order; quantities must be nonnegative.
// It does not check balance, concurrency, partner validity or persist reservations.
func PickTier(tiers []Tier, request TierRequest) (int, bool) {
	if (request.Rule != Paired && request.Rule != Distributed) || request.Increment <= 0 {
		return 0, false
	}
	floor := 0
	for index, tier := range tiers {
		if !tier.Start.After(request.Now) {
			floor = index
		}
	}
	for index := floor; index < len(tiers); index++ {
		tier := tiers[index]
		if request.blocked(tier) {
			break
		}
		if request.Couple && tier.Promo {
			continue
		}
		limit := capacity(tier, request.Rule)
		if limit <= 0 || !request.effective(tiers, index) {
			continue
		}
		used := request.used(tiers, index)
		if used+request.Increment <= limit {
			return index, true
		}
		if request.Rule == Distributed && request.Couple && request.Increment == 2 && used+1 == limit &&
			request.following(tiers[index+1:]) {
			return index, true
		}
	}
	return 0, false
}
