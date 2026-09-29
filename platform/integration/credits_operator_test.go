package integration_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsAggregateCompletePagingAndUnsentRelease(t *testing.T) {
	t.Parallel()
	s := credits.Service{DB: database(t)}
	_, err := s.DB.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	for index := range 25 {
		attempt := creditAttempt("alice")
		attempt.Scope.Key = fmt.Sprintf("campaign:fixture:%02d", index)
		require.NoError(t, s.Reserve(t.Context(), attempt))
		if index == 0 {
			change := credits.OperatorChange{
				Kind:      "release_unsent",
				Key:       "release:1",
				AttemptID: attempt.ID,
				Evidence:  "synthetic confirmed never sent",
			}
			require.NoError(t, s.Operate(t.Context(), "bob", change))
			require.ErrorIs(t, s.Dispatch(t.Context(), attempt.ID), credits.ErrConflict)
			continue
		}
		require.NoError(t, s.Dispatch(t.Context(), attempt.ID))
	}
	var period time.Time
	require.NoError(t, s.DB.QueryRow(t.Context(), `SELECT period_start FROM credits.attempts LIMIT 1`).Scan(&period))
	page, err := s.Aggregate(t.Context(), "bob", period, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 20)
	require.True(t, page.More)
	next, err := s.Aggregate(t.Context(), "bob", period, page.NextCursor)
	require.NoError(t, err)
	require.Len(t, next.Items, 4)
	require.False(t, next.More)
	seen := map[string]bool{}
	for _, item := range append(page.Items, next.Items...) {
		require.False(t, seen[item.OperationKey])
		seen[item.OperationKey] = true
		require.Equal(t, int64(1), item.Unknown)
	}
}

func TestCreditsOperatorImmutablePrice(t *testing.T) {
	t.Parallel()
	s := credits.Service{DB: database(t)}
	_, err := s.DB.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	change := credits.OperatorChange{
		Kind:     "register_price",
		Key:      "price:1",
		Evidence: "synthetic reviewed price and bound",
		Revision: &credits.PriceRevision{
			Provider: "openai",
			Model:    "fixture-asr",
			Source:   "synthetic provider",
			Price: credits.Price{
				Unit:              credits.UnitAudioSeconds,
				Version:           "fixture-1",
				Currency:          "USD",
				ConversionVersion: "identity",
				Conversion:        "1",
				AudioSecond:       "0.001",
				HardAudioSeconds:  "100",
				BoundSource:       "synthetic hard maximum",
			},
		},
	}
	require.NoError(t, s.Operate(t.Context(), "bob", change))
	require.NoError(t, s.Operate(t.Context(), "bob", change))
	change.Key = "price:2"
	change.Revision.Price.AudioSecond = "0.002"
	require.ErrorIs(t, s.Operate(t.Context(), "bob", change), credits.ErrConflict)
	selectPrice := credits.OperatorChange{
		Kind:     "select_price",
		Key:      "select:1",
		Evidence: "synthetic activation",
		Provider: "openai",
		Model:    "fixture-asr",
		Version:  "fixture-1",
	}
	require.NoError(t, s.Operate(t.Context(), "bob", selectPrice))
}
