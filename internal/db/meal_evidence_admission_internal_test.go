package db

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func TestMealEvidenceUsesArrivalDeadlineDespiteDelayedProcessing(t *testing.T) {
	t.Parallel()
	due := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		kind      OrderKind
		sent, now time.Time
		state     string
	}{
		{"meal_timely_delayed", OrderKindMeal, due.Add(-time.Second), due.Add(time.Hour), OrderEvidencePending},
		{"meal_timely", OrderKindMeal, due.Add(-time.Second), due.Add(-time.Second), OrderEvidencePending},
		{"meal_at_deadline", OrderKindMeal, due, due.Add(time.Hour), OrderEvidenceNeedsDecision},
		{"meal_late", OrderKindMeal, due.Add(time.Second), due.Add(time.Hour), OrderEvidenceNeedsDecision},
		{"service_processing_cutoff_preserved", OrderKindService, due.Add(-time.Second), due.Add(time.Hour), OrderEvidenceNeedsDecision},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			order := SubmittedOrder{OrderKind: tc.kind, Snapshot: OrderSnapshot{SalesClosesAt: due,
				Quote:    OrdersQuoteView{Quote: orders.Quote{TotalMinor: 100}},
				Capacity: &orders.CapacitySnapshot{Version: 1, Demands: []orders.ResourceDemand{}},
			}}
			state, reason, err := orderEvidenceAdmission(
				t.Context(),
				nil,
				OrderEvidenceSubmission{AmountMinor: 100},
				order,
				tc.sent,
				tc.now,
			)
			require.NoError(t, err)
			require.Equal(t, tc.state, state)
			if state == OrderEvidencePending {
				require.Empty(t, reason)
			} else {
				require.Equal(t, "deadline_requires_decision", reason)
			}
		})
	}
}
