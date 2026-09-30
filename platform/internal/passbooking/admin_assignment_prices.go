package passbooking

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

func (s *snapshot) adminPrices(c AdminAssignment, targets []*Booking) error {
	prices := make([]int, len(targets))
	indexes := make([]*int, len(targets))
	stats := s.stats()
	for index, b := range targets {
		price, tier, err := s.adminPrice(c, b, len(targets), index, stats)
		if err != nil {
			return err
		}
		prices[index], indexes[index] = price, tier
	}
	split := len(targets) > 1 && (prices[0] == 0 || prices[1] == 0)
	for index, b := range targets {
		s.applyAdminPrice(c, b, prices[index], indexes[index])
		if split {
			s.unlink(b)
		}
	}
	return nil
}

func (s *snapshot) adminPrice(c AdminAssignment, b *Booking, count, index int, stats statistics) (int, *int, error) {
	if c.TotalPrice != nil {
		price := *c.TotalPrice / count
		if index < *c.TotalPrice%count {
			price++
		}
		return price, nil, nil
	}
	if b.State == assigned || b.State == paid {
		if b.Price == nil {
			return 0, nil, conflict("pass_booking_state")
		}
		return *b.Price, nil, nil
	}
	usage, increment := stats.total, count
	if s.event.rule == passallocation.Paired {
		usage, increment = stats.roles[b.Role], 1
	}
	tier, ok := passallocation.PickTier(s.event.tiers, passallocation.TierRequest{Rule: s.event.rule, Usage: usage,
		Increment: increment, Couple: count > 1, IgnoreDateBlocks: true, Now: s.now})
	if !ok {
		return 0, nil, conflict("pass_tier_unavailable")
	}
	return s.event.tiers[tier].Price, &tier, nil
}

func (s *snapshot) applyAdminPrice(c AdminAssignment, b *Booking, price int, tier *int) {
	if b.State == waitlist || c.TotalPrice != nil {
		b.State = assigned
		if price == 0 {
			b.State = paid
		}
		b.Price = &price
		b.AssignedAt = &s.now
	}
	if b.TierIndex == nil && tier != nil {
		b.TierIndex = tier
	}
	if c.Kind != nil {
		b.Kind = *c.Kind
	}
	if c.Comment != nil {
		b.Comment = *c.Comment
	}
	if c.SkipBalance != nil {
		b.SkipBalance = c.SkipBalance
	}
	s.touch(b)
}
func (s *snapshot) appendAdminTier(
	ctx context.Context,
	tx pgx.Tx,
	c AdminAssignment,
	count int,
	before map[string]*Booking,
) error {
	if c.AppendTier == nil {
		return nil
	}
	prior := newSnapshot(s.event, before, s.now)
	current := prior.currentAdminTier()
	tier := *c.AppendTier - 1
	if current == 0 || *c.AppendTier > current || tier >= len(s.event.tiers) {
		return conflict("pass_tier_invalid")
	}
	const maxAmount = 1000000
	if s.event.tiers[tier].Amount > maxAmount-count {
		return conflict("pass_tier_invalid")
	}
	_, err := tx.Exec(
		ctx,
		`UPDATE core.pass_event_tiers SET amount=amount+$3 WHERE event_id=$1 AND position=$2`,
		c.Event,
		tier,
		count,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	s.event.tiers[tier].Amount += count
	return nil
}

func (s *snapshot) currentAdminTier() int {
	return currentAdminTier(s.event, s.stats(), s.now)
}

func currentAdminTier(e event, stats statistics, now time.Time) int {
	usages := []passallocation.Usage{stats.total}
	if e.rule == passallocation.Paired {
		usages = []passallocation.Usage{stats.roles[passallocation.Leader], stats.roles[passallocation.Follower]}
	}
	current := 0
	for _, usage := range usages {
		index, ok := passallocation.PickTier(
			e.tiers,
			passallocation.TierRequest{Rule: e.rule, Usage: usage, Increment: 1, Now: now},
		)
		if ok {
			current = max(current, index+1)
		}
	}
	if current == 0 {
		return len(e.tiers)
	}
	return current
}
