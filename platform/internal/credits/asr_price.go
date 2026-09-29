package credits

import "math/big"

const UnitASRTokens = "asr_tokens"
const UnitAudioSeconds = "audio_seconds"

// ASR usage has mutually exclusive duration and disjoint audio/text token forms.
// A revision names the billing unit explicitly; missing dimensions stay unpriced.
func (p Price) calculateASR(u Usage) (int64, string, error) {
	if u.Validate() != nil || p.Version == "" || p.ConversionVersion == "" || p.ServiceTier != "" {
		return 0, "", ErrInvalid
	}
	total := new(big.Rat)
	switch p.Unit {
	case UnitAudioSeconds:
		if u.AudioSeconds == nil || u.Input != nil || u.Output != nil {
			return 0, "", ErrInvalid
		}
		seconds, ok := boundedRatio(*u.AudioSeconds)
		rate, valid := boundedRatio(p.AudioSecond)
		if !ok || !valid {
			return 0, "", ErrInvalid
		}
		total.Mul(seconds, rate)
	case UnitASRTokens:
		if !p.applies(u) || u.AudioSeconds != nil || u.AudioInput == nil || u.TextInput == nil || u.Output == nil ||
			*u.AudioInput != *u.Input-*u.TextInput {
			return 0, "", ErrInvalid
		}
		for _, item := range []struct {
			count int64
			rate  string
		}{
			{*u.AudioInput, p.AudioInput}, {*u.TextInput, p.TextInput}, {*u.Output, p.Output},
		} {
			rate, ok := boundedRatio(item.rate)
			if !ok {
				return 0, "", ErrInvalid
			}
			total.Add(total, new(big.Rat).Mul(rate, new(big.Rat).SetInt64(item.count)))
		}
	default:
		return 0, "", ErrInvalid
	}
	return p.convert(total)
}

// MaximumASRExposure uses operator-reviewed provider maxima, not a guessed
// seconds-to-token ratio or an expected transcript length.
func (p Price) MaximumASRExposure() (int64, error) {
	if p.BoundSource == "" || len(p.BoundSource) > 512 || p.ServiceTier != "" {
		return 0, ErrUnpriced
	}
	usage := Usage{Basis: basisEstimated}
	switch p.Unit {
	case UnitAudioSeconds:
		seconds, ok := boundedRatio(p.HardAudioSeconds)
		if !ok || seconds.Sign() <= 0 {
			return 0, ErrUnpriced
		}
		usage.AudioSeconds = &p.HardAudioSeconds
	case UnitASRTokens:
		if p.MinInput != 0 || p.HardInputLimit <= 0 || p.HardInputLimit > p.MaxInput || p.HardOutputLimit <= 0 {
			return 0, ErrUnpriced
		}
		audio, ok := boundedRatio(p.AudioInput)
		text, valid := boundedRatio(p.TextInput)
		if !ok || !valid {
			return 0, ErrUnpriced
		}
		zero := int64(0)
		usage.Input, usage.Output = &p.HardInputLimit, &p.HardOutputLimit
		usage.AudioInput, usage.TextInput = &p.HardInputLimit, &zero
		if text.Cmp(audio) > 0 {
			usage.AudioInput, usage.TextInput = &zero, &p.HardInputLimit
		}
	default:
		return 0, ErrUnpriced
	}
	return p.Estimate(usage)
}
