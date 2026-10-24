package registrations

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"time"
)

const (
	PricingPaired      = "paired"
	PricingDistributed = "distributed"
	pricingPairSize    = 2
)

var tierID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// TierPolicy retains configured order, including promo and zero-capacity tiers.
type TierPolicy struct {
	Mode  string `json:"mode"`
	Tiers []Tier `json:"tiers"`
}

type Tier struct {
	ID            string    `json:"id"`
	Amount        int32     `json:"amount"`
	PriceMinor    int64     `json:"price_minor"`
	StartsAt      time.Time `json:"starts_at"`
	Promo         bool      `json:"promo" required:"false"`
	BlockedByDate bool      `json:"blocked_by_date" required:"false"`
}

func (t *Tier) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return ErrInvalid
	}
	for _, name := range []string{"id", "amount", "price_minor", "starts_at"} {
		value, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrInvalid
		}
	}
	type document Tier
	var decoded document
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&decoded); err != nil {
		return ErrInvalid
	}
	*t = Tier(decoded)
	return nil
}

func (p TierPolicy) Normalize() (TierPolicy, error) {
	if (p.Mode != PricingPaired && p.Mode != PricingDistributed) || len(p.Tiers) == 0 || len(p.Tiers) > 100 {
		return TierPolicy{}, ErrInvalid
	}
	p.Tiers = append([]Tier(nil), p.Tiers...)
	seen := make(map[string]bool, len(p.Tiers))
	for i, tier := range p.Tiers {
		if !tierID.MatchString(tier.ID) || seen[tier.ID] || tier.Amount < 0 || tier.PriceMinor <= 0 ||
			tier.PriceMinor > maxJSONInteger ||
			tier.StartsAt.IsZero() ||
			tier.StartsAt.Nanosecond()%int(time.Microsecond) != 0 {
			return TierPolicy{}, ErrInvalid
		}
		seen[tier.ID] = true
		p.Tiers[i].StartsAt = tier.StartsAt.UTC()
	}
	return p, nil
}

// TierSnapshot freezes a stable ID and its one-based configured ordinal.
type TierSnapshot struct {
	ID     string `json:"id"`
	Number int32  `json:"number"`
}

type PriceQuote struct {
	PriceMinor int64         `json:"price_minor"`
	Tier       *TierSnapshot `json:"tier,omitempty"`
}

// TierUsage groups assigned owner passes. Empty TierID denotes legacy flat passes.
// Every row contributes to totals, even if its tier was removed from the policy.
type TierUsage struct {
	Role   string
	TierID string
	Count  int64
}

// ResolvePrices quotes one owner or an opposite-role pair in input order.
// The caller supplies one database instant and all assigned owner-pass counts.
func ResolvePrices(rules Rules, now time.Time, usage []TierUsage, roles []string) ([]PriceQuote, error) {
	if !validPricingRoles(roles) || now.IsZero() {
		return nil, ErrInvalid
	}
	rules, err := rules.Normalize()
	if err != nil {
		return nil, err
	}
	if rules.Pricing == nil {
		quotes := make([]PriceQuote, len(roles))
		for i := range quotes {
			quotes[i].PriceMinor = rules.PriceMinor
		}
		return quotes, nil
	}
	resolver, err := newTierResolver(*rules.Pricing, now, usage, len(roles) == pricingPairSize)
	if err != nil {
		return nil, err
	}
	return resolver.quotes(roles)
}

func validPricingRoles(roles []string) bool {
	if len(roles) < 1 || len(roles) > pricingPairSize || !ValidRole(roles[0]) {
		return false
	}
	return len(roles) == 1 || (ValidRole(roles[1]) && roles[0] != roles[1])
}

// tierResolver holds one immutable policy, instant and assigned-pass view.
// The empty role key contains totals across both roles for distributed pricing.
type tierResolver struct {
	policy   TierPolicy
	now      time.Time
	pair     bool
	counts   map[string]int64
	explicit map[string]map[string]int64
	prior    []int64
	numbers  []int32
	floor    int
}

func newTierResolver(policy TierPolicy, now time.Time, usage []TierUsage, pair bool) (tierResolver, error) {
	r := tierResolver{
		policy: policy, now: now, pair: pair,
		counts:   map[string]int64{},
		explicit: map[string]map[string]int64{"": {}, Leader: {}, Follower: {}},
		prior:    make([]int64, len(policy.Tiers)),
		numbers:  make([]int32, len(policy.Tiers)),
	}
	for _, row := range usage {
		if !ValidRole(row.Role) || row.Count < 0 || row.Count > math.MaxInt64-pricingPairSize-r.counts[""] {
			return tierResolver{}, ErrInvalid
		}
		r.counts[""] += row.Count
		r.counts[row.Role] += row.Count
		r.explicit[""][row.TierID] += row.Count
		r.explicit[row.Role][row.TierID] += row.Count
	}
	// Normalize bounds the policy to 100 tiers. An int32 counter preserves
	// that ordinal without converting an unbounded slice index.
	var number int32
	for i, tier := range policy.Tiers {
		number++
		r.numbers[i] = number
		if i > 0 {
			r.prior[i] = r.prior[i-1] + r.capacity(i-1)
		}
		if !tier.StartsAt.After(now) {
			r.floor = i
		}
	}
	return r, nil
}

func (r tierResolver) capacity(i int) int64 {
	n := int64(r.policy.Tiers[i].Amount)
	if r.policy.Mode == PricingPaired {
		n /= pricingPairSize
	}
	return n
}

func (r tierResolver) used(i int, role string) int64 {
	implied := max(int64(0), min(r.counts[role]-r.prior[i], r.capacity(i)))
	// Unsold capacity in expired tiers must not hide an explicit pair overflow.
	var overflow int64
	for j := range i {
		overflow = max(int64(0), overflow+r.explicit[role][r.policy.Tiers[j].ID]-r.capacity(j))
	}
	return max(r.explicit[role][r.policy.Tiers[i].ID]+overflow, implied)
}

func (r tierResolver) blocked(i int) bool {
	tier := r.policy.Tiers[i]
	return tier.BlockedByDate && tier.StartsAt.After(r.now)
}

func (r tierResolver) effective(i int, role string, increment int64) bool {
	if !r.policy.Tiers[i].StartsAt.After(r.now) {
		return true
	}
	if i == 0 {
		return false
	}
	if r.prior[i] < r.counts[role]+increment {
		return true
	}
	for j := i - 1; j >= 0; j-- {
		if r.capacity(j) > 0 {
			return r.used(j, role) >= r.capacity(j)
		}
	}
	return false
}

func (r tierResolver) hasFollowingAssignable(i int) bool {
	for j := i + 1; j < len(r.policy.Tiers); j++ {
		if r.blocked(j) {
			break
		}
		if !r.policy.Tiers[j].Promo && r.capacity(j) > 0 {
			return true
		}
	}
	return false
}

func (r tierResolver) permitsOverflow(i int, used int64) bool {
	return r.policy.Mode == PricingDistributed && r.pair && used+1 == r.capacity(i) && r.hasFollowingAssignable(i)
}

func (r tierResolver) pick(role string, increment int64) int {
	for i := r.floor; i < len(r.policy.Tiers); i++ {
		if r.blocked(i) {
			break
		}
		if (r.pair && r.policy.Tiers[i].Promo) || r.capacity(i) == 0 {
			continue
		}
		if !r.effective(i, role, increment) {
			continue
		}
		n := r.used(i, role)
		if n+increment <= r.capacity(i) || r.permitsOverflow(i, n) {
			return i
		}
	}
	return -1
}

func (r tierResolver) quotes(roles []string) ([]PriceQuote, error) {
	quotes := make([]PriceQuote, len(roles))
	for i, role := range roles {
		increment := int64(1)
		if r.policy.Mode == PricingDistributed {
			role = ""
			increment = int64(len(roles))
		}
		index := r.pick(role, increment)
		if index < 0 {
			return nil, ErrCapacity
		}
		quotes[i] = PriceQuote{
			PriceMinor: r.policy.Tiers[index].PriceMinor,
			Tier:       &TierSnapshot{ID: r.policy.Tiers[index].ID, Number: r.numbers[index]},
		}
	}
	return quotes, nil
}
