package credits_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestASRDisjointRatesAndReviewedExposure(t *testing.T) {
	t.Parallel()
	p := credits.Price{Unit: credits.UnitASRTokens, Version: "fixture", ConversionVersion: "identity", Conversion: "1",
		AudioInput: "0.001", TextInput: "0.002", Output: "0.003", HardInputLimit: 100, MaxInput: 100,
		HardOutputLimit: 10, BoundSource: "synthetic provider maximum"}
	u := credits.Usage{
		Basis:      "reported",
		Input:      new(int64(10)),
		AudioInput: new(int64(8)),
		TextInput:  new(int64(2)),
		Output:     new(int64(3)),
	}
	cost, err := p.Estimate(u)
	require.NoError(t, err)
	require.Equal(t, int64(21_000_000), cost)
	bound, err := p.MaximumASRExposure()
	require.NoError(t, err)
	require.Equal(t, int64(230_000_000), bound)
	u.TextInput = nil
	_, err = p.Estimate(u)
	require.ErrorIs(t, err, credits.ErrInvalid)
	p.HardOutputLimit = 0
	_, err = p.MaximumASRExposure()
	require.ErrorIs(t, err, credits.ErrUnpriced)
}

func TestASRDurationExactRoundingAndMissingBound(t *testing.T) {
	t.Parallel()
	p := credits.Price{Unit: credits.UnitAudioSeconds, Version: "fixture", ConversionVersion: "fx", Conversion: "1/3",
		AudioSecond: "0.000000001", HardAudioSeconds: "300", BoundSource: "synthetic duration cap"}
	seconds := "1.25"
	cost, err := p.Estimate(credits.Usage{Basis: "reported", AudioSeconds: &seconds})
	require.NoError(t, err)
	require.Equal(t, int64(1), cost)
	bound, err := p.MaximumASRExposure()
	require.NoError(t, err)
	require.Equal(t, int64(100), bound)
	p.HardAudioSeconds = ""
	_, err = p.MaximumASRExposure()
	require.ErrorIs(t, err, credits.ErrUnpriced)
}
