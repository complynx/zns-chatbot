package credits

// MaximumExposure uses a reviewed provider hard context bound and the actual
// request output cap. It assumes the most expensive input category, never cache
// savings. Unsupported modalities and missing bounds fail closed.
func (p Price) MaximumExposure(outputLimit int64) (int64, error) {
	if p.Unit != "" || p.ServiceTier != "default" || p.MinInput != 0 || p.BoundSource == "" ||
		len(p.BoundSource) > 512 {
		return 0, ErrUnpriced
	}
	if p.HardInputLimit <= 0 || p.HardInputLimit > p.MaxInput || p.HardOutputLimit <= 0 ||
		outputLimit <= 0 || outputLimit > p.HardOutputLimit {
		return 0, ErrUnpriced
	}
	highest, ok := boundedRatio(p.Input)
	if !ok {
		return 0, ErrUnpriced
	}
	rates := []string{p.Cached}
	if p.CacheWrites {
		rates = append(rates, p.CacheWrite)
	}
	for _, raw := range rates {
		rate, valid := boundedRatio(raw)
		if !valid {
			return 0, ErrUnpriced
		}
		if rate.Cmp(highest) > 0 {
			highest = rate
		}
	}
	bound := p
	bound.Input = highest.RatString()
	zero := int64(0)
	return bound.Estimate(
		Usage{
			Basis:       basisEstimated,
			ServiceTier: p.ServiceTier,
			Input:       &p.HardInputLimit,
			Cached:      &zero,
			CacheWrite:  &zero,
			Output:      &outputLimit,
		},
	)
}
