package credits

import "math/big"

// Price is an immutable per-token rate in original currency. Conversion maps
// that currency to USD. No live rate or default conversion is inferred.
type Price struct {
	Unit              string `json:"unit,omitempty"`
	AudioInput        string `json:"audio_input,omitempty"`
	TextInput         string `json:"text_input,omitempty"`
	AudioSecond       string `json:"audio_second,omitempty"`
	HardAudioSeconds  string `json:"hard_audio_seconds,omitempty"`
	HardInputLimit    int64  `json:"hard_input_limit,omitempty"`
	HardOutputLimit   int64  `json:"hard_output_limit,omitempty"`
	BoundSource       string `json:"bound_source,omitempty"`
	ServiceTier       string `json:"service_tier,omitempty"`
	MaxInput          int64  `json:"max_input_tokens,omitempty"`
	MinInput          int64  `json:"min_input_tokens,omitempty"`
	Currency          string `json:"currency"`
	Version           string `json:"version"`
	ConversionVersion string `json:"conversion_version"`
	Input             string `json:"input"`
	Cached            string `json:"cached"`
	CacheWrite        string `json:"cache_write"`
	Output            string `json:"output"`
	CacheWrites       bool   `json:"cache_writes"`
	Conversion        string `json:"conversion"`
}

const nanoUSDPerDollar = 1_000_000_000

func (p Price) Estimate(u Usage) (int64, error) {
	amount, _, err := p.calculate(u)
	return amount, err
}

func (p Price) calculate(u Usage) (int64, string, error) {
	if p.Unit != "" {
		return p.calculateASR(u)
	}
	if !p.applies(u) {
		return 0, "", ErrInvalid
	}
	if u.Validate() != nil || p.Version == "" || p.ConversionVersion == "" || u.Input == nil || u.Cached == nil ||
		u.Output == nil {
		return 0, "", ErrInvalid
	}
	write := int64(0)
	if p.CacheWrites {
		if u.CacheWrite == nil {
			return 0, "", ErrInvalid
		}
		write = *u.CacheWrite
	}
	if !p.CacheWrites && u.CacheWrite != nil && *u.CacheWrite != 0 {
		return 0, "", ErrInvalid
	}
	total := new(big.Rat)
	for _, item := range []struct {
		count int64
		rate  string
	}{{*u.Input - *u.Cached - write, p.Input}, {*u.Cached, p.Cached}, {write, p.CacheWrite}, {*u.Output, p.Output}} {
		rate, ok := boundedRatio(item.rate)
		if !ok || rate.Sign() < 0 {
			return 0, "", ErrInvalid
		}
		total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(item.count)))
	}
	return p.convert(total)
}

func (p Price) convert(total *big.Rat) (int64, string, error) {
	conversion, ok := boundedRatio(p.Conversion)
	if !ok || conversion.Sign() <= 0 {
		return 0, "", ErrInvalid
	}
	original := total.RatString()
	total.Mul(total, conversion)
	total.Mul(total, new(big.Rat).SetInt64(nanoUSDPerDollar))
	amount, remainder := new(big.Int), new(big.Int)
	amount.QuoRem(total.Num(), total.Denom(), remainder)
	if remainder.Sign() > 0 {
		amount.Add(amount, big.NewInt(1))
	}
	if !amount.IsInt64() {
		return 0, "", ErrInvalid
	}
	return amount.Int64(), original, nil
}

func (p Price) applies(u Usage) bool {
	if p.ServiceTier != "" && p.ServiceTier != u.ServiceTier {
		return false
	}
	if p.MinInput < 0 || p.MaxInput < 0 || (p.MaxInput > 0 && p.MinInput > p.MaxInput) {
		return false
	}
	if u.Input == nil {
		return false
	}
	return *u.Input >= p.MinInput && (p.MaxInput == 0 || *u.Input <= p.MaxInput)
}

// Reject exponent notation before [big.Rat] parsing: short exponent strings can
// otherwise request arbitrarily large allocations.
func boundedRatio(value string) (*big.Rat, bool) {
	if value == "" || len(value) > 64 {
		return nil, false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && char != '.' && char != '/' {
			return nil, false
		}
	}
	return new(big.Rat).SetString(value)
}
