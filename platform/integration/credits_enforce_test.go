package integration_test

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func creditAttempt(payer string) credits.Attempt {
	return credits.Attempt{
		ID:          uuid.NewString(),
		Scope:       credits.Scope{Actor: payer, Payer: payer, Key: "synthetic:1"},
		Operation:   "test.plan",
		Provider:    "openai",
		Model:       "synthetic-bound",
		OutputLimit: 10,
	}
}

func installCreditPrice(t *testing.T, service credits.Service) {
	t.Helper()
	price := credits.Price{
		Version:           "synthetic-v1",
		Currency:          "USD",
		ConversionVersion: "synthetic-fx",
		Conversion:        "1",
		Input:             "0.006",
		Cached:            "0.001",
		CacheWrite:        "0",
		Output:            "0",
		ServiceTier:       "default",
		MaxInput:          100,
		HardInputLimit:    100,
		HardOutputLimit:   10,
		BoundSource:       "synthetic fixture guarantee, not a production price",
	}
	require.NoError(
		t,
		service.RegisterPrice(
			t.Context(),
			credits.PriceRevision{Provider: "openai", Model: "synthetic-bound", Source: "test fixture", Price: price},
		),
	)
	require.NoError(t, service.SelectPrice(t.Context(), "openai", "synthetic-bound", price.Version))
}

func TestCreditsConcurrentReservationAndUnknownHold(t *testing.T) {
	t.Parallel()
	service := credits.Service{DB: database(t), Enforce: true}
	installCreditPrice(t, service)
	attempts := []credits.Attempt{creditAttempt("alice"), creditAttempt("alice")}
	type outcome struct {
		index int
		err   error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for index := range attempts {
		wg.Go(func() { results <- outcome{index, service.Reserve(t.Context(), attempts[index])} })
	}
	wg.Wait()
	close(results)
	winners := []int{}
	for result := range results {
		if result.err == nil {
			winners = append(winners, result.index)
		} else {
			require.ErrorIs(t, result.err, credits.ErrLimit)
		}
	}
	require.Len(t, winners, 1)
	winner := attempts[winners[0]]
	require.NoError(t, service.Reserve(t.Context(), winner))
	require.NoError(t, service.Dispatch(t.Context(), winner.ID))
	require.NoError(
		t,
		service.Settle(
			t.Context(),
			winner.ID,
			credits.Settlement{Usage: credits.Usage{Basis: "unknown"}, CostBasis: "unknown"},
		),
	)
	usage, err := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, int64(600_000_000), usage.HeldNanoUSD)
	require.Equal(t, int64(400_000_000), *usage.AvailableNanoUSD)
	require.ErrorIs(t, service.Reserve(t.Context(), creditAttempt("alice")), credits.ErrLimit)
}

func TestCreditsUnpricedAndCurrentRoleRecheck(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db, Enforce: true}
	attempt := creditAttempt("alice")
	require.ErrorIs(t, service.Reserve(t.Context(), attempt), credits.ErrUnpriced)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	require.NoError(t, service.Reserve(t.Context(), attempt))
	usage, err := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.True(t, usage.Policy.ImplicitUnlimited)
	require.Equal(t, int64(1), usage.UnboundedUnknown)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
	require.NoError(t, err)
	require.ErrorIs(t, service.Dispatch(t.Context(), attempt.ID), credits.ErrUnpriced)
	var state string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state FROM credits.attempts WHERE id=$1`, attempt.ID).Scan(&state),
	)
	require.Equal(t, "not_sent", state)
}

func TestCreditsPeriodAndPolicyShrinkBeforeDispatch(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db, Enforce: true}
	installCreditPrice(t, service)
	attempt := creditAttempt("alice")
	require.NoError(t, service.Reserve(t.Context(), attempt))
	_, err := db.Exec(
		t.Context(),
		`UPDATE credits.attempts SET period_start=period_start-interval '1 month' WHERE id=$1`,
		attempt.ID,
	)
	require.NoError(t, err)
	require.ErrorIs(t, service.Dispatch(t.Context(), attempt.ID), credits.ErrConflict)
	attempt = creditAttempt("alice")
	require.NoError(t, service.Reserve(t.Context(), attempt))
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	amount := int64(500_000_000)
	change := credits.PolicyChange{MonthlyNanoUSD: &amount, Version: 1, OperationKey: "policy:1"}
	policy, err := service.SetPolicy(t.Context(), "bob", "alice", change)
	require.NoError(t, err)
	replay, err := service.SetPolicy(t.Context(), "bob", "alice", change)
	require.NoError(t, err)
	require.Equal(t, policy, replay)
	// A lost manual reply can refresh the current version before replaying the
	// same operation. That precondition is not a second policy mutation.
	change.Version = policy.Version
	replay, err = service.SetPolicy(t.Context(), "bob", "alice", change)
	require.NoError(t, err)
	require.Equal(t, policy, replay)
	require.ErrorIs(t, service.Dispatch(t.Context(), attempt.ID), credits.ErrLimit)
	usage, err := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Zero(t, usage.HeldNanoUSD)
	require.Equal(t, amount, *usage.AvailableNanoUSD)
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.SetPolicy(t.Context(), "bob", "alice", change)
	requireCode(t, err, "forbidden")
}

func TestCreditsTruthfulOverrunBlocksNextDispatch(t *testing.T) {
	t.Parallel()
	service := credits.Service{DB: database(t), Enforce: true}
	installCreditPrice(t, service)
	attempt := creditAttempt("alice")
	require.NoError(t, service.Reserve(t.Context(), attempt))
	require.NoError(t, service.Dispatch(t.Context(), attempt.ID))
	cost := int64(1_200_000_000)
	require.NoError(
		t,
		service.Settle(
			t.Context(),
			attempt.ID,
			credits.Settlement{
				Usage:       credits.Usage{Basis: "unknown"},
				CostBasis:   "provider_reported",
				CostNanoUSD: &cost,
			},
		),
	)
	report, err := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, cost, report.SpentNanoUSD)
	require.Zero(t, *report.AvailableNanoUSD)
	require.ErrorIs(t, service.Reserve(t.Context(), creditAttempt("alice")), credits.ErrLimit)
}
