package passbooking

import (
	"cmp"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

type statistics struct {
	balance passallocation.Counts
	waiting passallocation.Counts
	unpaid  int
	total   passallocation.Usage
	roles   map[passallocation.Role]passallocation.Usage
}

func addRole(counts *passallocation.Counts, role passallocation.Role) {
	if role == passallocation.Leader {
		counts.Leader++
	} else {
		counts.Follower++
	}
}

func (s *snapshot) stats() statistics {
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

func ordered(a, b *Booking) int {
	if order := a.CreatedAt.Compare(b.CreatedAt); order != 0 {
		return order
	}
	return cmp.Compare(a.TelegramID, b.TelegramID)
}

func (s *snapshot) waitlists() map[passallocation.Role][]*Booking {
	lists := map[passallocation.Role][]*Booking{}
	for _, b := range s.bookings {
		if b.State == waitlist {
			lists[b.Role] = append(lists[b.Role], b)
		}
	}
	for _, list := range lists {
		slices.SortFunc(list, ordered)
	}
	return lists
}

func (s *snapshot) allocate() {
	for s.assignNext() {
	}
}

func (s *snapshot) assignNext() bool {
	stats := s.stats()
	lists := s.waitlists()
	leader, follower := lists[passallocation.Leader], lists[passallocation.Follower]
	if len(leader)+len(follower) == 0 {
		return false
	}
	target := stats.balance.TargetRole(stats.waiting)
	if !stats.balance.Balanced() && len(lists[target]) > 0 && lists[target][0].Partner == "" &&
		s.tryCandidate(lists[target][0], true) {
		return true
	}
	if s.doubleSolo(leader, follower, stats) {
		return true
	}
	heads := []*Booking{}
	for _, role := range []passallocation.Role{passallocation.Leader, passallocation.Follower} {
		if len(lists[role]) > 0 && lists[role][0].Partner != "" {
			heads = append(heads, lists[role][0])
		}
	}
	slices.SortFunc(heads, ordered)
	if len(leader) > 0 && len(follower) > 0 && len(heads) > 0 && s.tryCandidate(heads[0], true) {
		return true
	}
	other := passallocation.Leader
	if target == passallocation.Leader {
		other = passallocation.Follower
	}
	if s.firstSolo(lists[target]) || s.firstSolo(lists[other]) {
		return true
	}
	return len(heads) > 0 && s.tryCandidate(heads[0], true)
}

func (s *snapshot) firstSolo(list []*Booking) bool {
	for _, b := range list {
		if b.Partner == "" {
			return s.tryCandidate(b, true)
		}
	}
	return false
}

func (s *snapshot) doubleSolo(leader, follower []*Booking, stats statistics) bool {
	const pairSize = 2
	if len(leader) == 0 || len(follower) == 0 || leader[0].Partner != "" || follower[0].Partner != "" {
		return false
	}
	if !stats.balance.Allows(passallocation.Counts{Leader: 1, Follower: 1}, true) ||
		!passallocation.HasConcurrencyCapacity(stats.unpaid, pairSize, s.event.unlimited) {
		return false
	}
	// Python permits the first assignment to remain if the second cannot be priced.
	first := s.tryCandidate(leader[0], false)
	second := s.tryCandidate(follower[0], false)
	return first || second
}

func (s *snapshot) candidates(b *Booking) []*Booking {
	if b.Partner == "" {
		return []*Booking{b}
	}
	partner := s.bookings[b.Partner]
	// A pending invitation is not an accepted pair and cannot be auto-assigned.
	if partner != nil && partner.State == pending {
		return nil
	}
	if partner != nil && partner.State == waitlist && partner.Partner == b.Owner && partner.Role != b.Role {
		return []*Booking{b, partner}
	}
	// Historical broken links can recover as solo; imported records need separate validation.
	return []*Booking{b}
}

func (s *snapshot) tryCandidate(b *Booking, checkBalance bool) bool {
	participants := s.candidates(b)
	if len(participants) == 0 {
		return false
	}
	stats := s.stats()
	var delta passallocation.Counts
	for _, participant := range participants {
		addRole(&delta, participant.Role)
	}
	if !passallocation.HasConcurrencyCapacity(stats.unpaid, len(participants), s.event.unlimited) ||
		(checkBalance && !stats.balance.Allows(delta, false)) {
		return false
	}
	indexes := make([]int, len(participants))
	for i, participant := range participants {
		usage, increment := stats.total, len(participants)
		if s.event.rule == passallocation.Paired {
			usage = stats.roles[participant.Role]
			increment = 1
		}
		index, ok := passallocation.PickTier(
			s.event.tiers,
			passallocation.TierRequest{
				Rule:      s.event.rule,
				Usage:     usage,
				Increment: increment,
				Couple:    len(participants) > 1,
				Now:       s.now,
			},
		)
		if !ok {
			return false
		}
		indexes[i] = index
	}
	for i, participant := range participants {
		if len(participants) == 1 && participant.Partner != "" {
			s.unlink(participant)
		}
		participant.State = assigned
		participant.AssignedAt = &s.now
		price := s.event.tiers[indexes[i]].Price
		participant.Price = &price
		if participant.TierIndex == nil {
			index := indexes[i]
			participant.TierIndex = &index
		}
		s.touch(participant)
	}
	return true
}
