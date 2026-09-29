package passallocation

// TierDetail describes allocator usage; Left also includes unsold earlier tiers.
type TierDetail struct {
	Number        int  `json:"number"`
	Tier          Tier `json:"tier"`
	Capacity      int  `json:"capacity"`
	Used          int  `json:"used"`
	Explicit      int  `json:"explicit"`
	Left          int  `json:"left"`
	Overflow      bool `json:"overflow"`
	PriorCapacity int  `json:"prior_capacity"`
	Participants  int  `json:"participants"`
	Future        bool `json:"future"`
}

type TierReport struct {
	Current *TierDetail `json:"current,omitempty"`
	Next    *TierDetail `json:"next,omitempty"`
}

func tierDetail(tiers []Tier, request TierRequest, index int) *TierDetail {
	limit := capacity(tiers[index], request.Rule)
	used := request.used(tiers, index)
	left := max(0, limit-used)
	for previous := range index {
		left += max(0, capacity(tiers[previous], request.Rule)-request.used(tiers, previous))
	}
	return &TierDetail{Number: index + 1, Tier: tiers[index], Capacity: limit, Used: used,
		Explicit: request.Usage.Explicit[index], Left: left,
		Overflow:      request.Couple && used+request.Increment > limit,
		PriorCapacity: priorCapacity(tiers, index, request.Rule), Participants: request.Usage.Participants,
		Future: tiers[index].Start.After(request.Now)}
}

// DescribeTiers reports the same eligibility as PickTier, then the next configured
// capacity when no tier is assignable. It never changes allocation state.
func DescribeTiers(tiers []Tier, request TierRequest) TierReport {
	if index, ok := PickTier(tiers, request); ok {
		return TierReport{Current: tierDetail(tiers, request, index)}
	}
	floor := 0
	for index, tier := range tiers {
		if !tier.Start.After(request.Now) {
			floor = index
		}
	}
	for index := floor; index < len(tiers); index++ {
		if capacity(tiers[index], request.Rule) > 0 &&
			(request.used(tiers, index) < capacity(tiers[index], request.Rule) || tiers[index].Start.After(request.Now)) {
			return TierReport{Next: tierDetail(tiers, request, index)}
		}
	}
	return TierReport{}
}
