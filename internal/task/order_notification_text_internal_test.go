package task

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestOrderNotificationTextKeepsMoneyStatesAndEventDSTDistinct(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, kind := range []string{db.OrderEvidencePending, db.OrderEvidenceExhausted, db.OrderEvidenceNeedsDecision, db.OrderEvidenceConfirmed, db.OrderEvidenceRejected} {
			t.Run(language+"/"+kind, func(t *testing.T) {
				t.Parallel()
				n := db.OrderNotification{Kind: kind, Evidence: db.OrderPaymentEvidence{
					ID:            uuid.New(),
					OrderID:       uuid.New(),
					EventID:       uuid.New(),
					AmountMinor:   1234,
					Currency:      "BYN",
					CurrencyScale: 2,
					SentAt: time.Date(
						2026,
						time.October,
						25,
						0,
						30,
						0,
						0,
						time.UTC,
					),
					DueAt: time.Date(2026, time.October, 25, 1, 30, 0, 0, time.UTC),
				}}
				n.Order.Snapshot.EventName = "<b>literal event</b>"
				n.Order.Snapshot.Quote.Timezone = "Europe/Berlin"
				if kind == db.OrderEvidenceConfirmed || kind == db.OrderEvidenceRejected {
					n.Evidence.Review = &db.OrderPaymentDecision{
						Reason:     "<script>literal reason</script>",
						ReviewedAt: n.Evidence.DueAt.Add(time.Hour),
					}
				}
				owner, err := orderNotificationText(n, language, false)
				require.NoError(t, err)
				require.Contains(t, owner, "12.34 BYN")
				require.Contains(t, owner, "02:30:00+02:00")
				require.Contains(t, owner, "02:30:00+01:00")
				require.Contains(t, owner, "Europe/Berlin")
				require.Contains(t, owner, "<b>literal event</b>")
				require.NotContains(t, owner, "Pass is fully paid")
				if n.Evidence.Review != nil {
					require.Contains(t, owner, "<script>literal reason</script>")
				}
				if kind == db.OrderEvidenceExhausted {
					reviewer, reviewErr := orderNotificationText(n, language, true)
					require.NoError(t, reviewErr)
					require.NotEqual(t, owner, reviewer)
				}
			})
		}
	}
}

func TestOrderNotificationTextRejectsMissingHumanDecisionOrUnknownKind(t *testing.T) {
	t.Parallel()
	for _, scale := range []int{-1, 7} {
		n := db.OrderNotification{Kind: db.OrderEvidencePending}
		n.Order.Snapshot.Quote.Timezone = "UTC"
		n.Evidence.CurrencyScale = scale
		_, err := orderNotificationText(n, "en", false)
		require.ErrorContains(t, err, "currency scale")
	}

	for _, kind := range []string{db.OrderEvidenceConfirmed, db.OrderEvidenceRejected, "unknown"} {
		n := db.OrderNotification{Kind: kind}
		n.Order.Snapshot.Quote.Timezone = "UTC"
		_, err := orderNotificationText(n, "en", false)
		require.Error(t, err)
	}
}
