package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsASRReserveAndSettle(t *testing.T) {
	t.Parallel()
	s := credits.Service{DB: database(t), Enforce: true}
	p := credits.Price{Unit: credits.UnitAudioSeconds, Version: "audio-fixture", Currency: "USD",
		ConversionVersion: "identity", Conversion: "1", AudioSecond: "0.001", HardAudioSeconds: "300",
		BoundSource: "synthetic provider maximum"}
	require.NoError(
		t,
		s.RegisterPrice(
			t.Context(),
			credits.PriceRevision{Provider: "openai", Model: "synthetic-bound", Source: "fixture", Price: p},
		),
	)
	require.NoError(t, s.SelectPrice(t.Context(), "openai", "synthetic-bound", p.Version))
	a := creditAttempt("alice")
	a.Operation, a.OutputLimit = "audio.transcribe", 0
	require.NoError(t, s.Reserve(t.Context(), a))
	require.NoError(t, s.Dispatch(t.Context(), a.ID))
	seconds := "12.5"
	require.NoError(t, s.Settle(t.Context(), a.ID, credits.Settlement{CostBasis: "unknown",
		Usage: credits.Usage{Model: a.Model, Basis: "reported", AudioSeconds: &seconds}}))
	r, err := s.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, int64(12_500_000), r.SpentNanoUSD)
	require.Zero(t, r.HeldNanoUSD)
	a = creditAttempt("alice")
	require.ErrorIs(t, s.Reserve(t.Context(), a), credits.ErrUnpriced, "ASR prices cannot fund Responses")
}
