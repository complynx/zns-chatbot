package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsAuditedReconciliationAndAdjustment(t *testing.T) {
	t.Parallel()
	s := credits.Service{DB: database(t), Enforce: true}
	installCreditPrice(t, s)
	_, err := s.DB.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('bob')`)
	require.NoError(t, err)
	a := creditAttempt("alice")
	require.NoError(t, s.Reserve(t.Context(), a))
	require.NoError(t, s.Dispatch(t.Context(), a.ID))
	change := credits.OperatorChange{
		Kind:      "reconcile",
		Key:       "invoice:1",
		AttemptID: a.ID,
		Amount:    new(int64(100000000)),
		Evidence:  "synthetic provider invoice fixture",
	}
	require.Error(t, s.Operate(t.Context(), "alice", change))
	require.NoError(t, s.Operate(t.Context(), "bob", change))
	require.NoError(t, s.Operate(t.Context(), "bob", change))
	change.Amount = new(int64(200000000))
	require.ErrorIs(t, s.Operate(t.Context(), "bob", change), credits.ErrConflict)
	report, err := s.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, int64(100000000), report.SpentNanoUSD)
	require.Zero(t, report.HeldNanoUSD)
	adjust := credits.OperatorChange{
		Kind:     "adjust",
		Key:      "correction:1",
		Payer:    "alice",
		Period:   report.PeriodStart,
		Amount:   new(int64(-50000000)),
		Evidence: "synthetic confirmed correction",
	}
	require.NoError(t, s.Operate(t.Context(), "bob", adjust))
	report, err = s.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, int64(50000000), report.SpentNanoUSD)
	page, err := s.Aggregate(t.Context(), "bob", report.PeriodStart, "")
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	_, err = s.Aggregate(t.Context(), "alice", report.PeriodStart, "")
	require.Error(t, err)
	var original *int64
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT cost_nano_usd FROM credits.attempts WHERE id=$1`, a.ID).Scan(&original),
	)
	require.Nil(t, original, "reconciliation must preserve original unknown receipt")
	_, err = s.DB.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	require.Error(t, s.Operate(t.Context(), "bob", adjust), "replay rechecks current authority")
}

func TestCreditsCutoverPreservesLegacyUpdate(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.CreditsEnforce = true
	handle(t, f.b, message(8501, 101, "Before cutover"))
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	s := credits.Service{DB: f.db, Enforce: true}
	require.NoError(
		t,
		s.Operate(
			t.Context(),
			"alice",
			credits.OperatorChange{
				Kind:     "cutover",
				Key:      "epoch:1",
				Evidence: "synthetic completed drain and operator activation",
			},
		),
	)
	handle(t, f.b, message(8502, 101, "After cutover"))
	handle(t, f.b, message(8501, 101, "Before cutover"))
	var modes []string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT array_agg(mode ORDER BY update_id) FROM bot.budget_operations WHERE owner='alice' AND update_id IN (8501,8502)`).
			Scan(&modes),
	)
	require.Equal(t, []string{"legacy", "credits"}, modes)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.agent_quota WHERE owner='alice' AND update_id=8502`).
			Scan(&count),
	)
	require.Zero(t, count)
	var epoch time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT epoch FROM credits.cutover`).Scan(&epoch))
	require.False(t, epoch.IsZero())
}
