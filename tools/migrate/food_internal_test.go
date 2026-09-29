package migrate

import (
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFoodSourceKeepsIndependentHistoricalPayments(t *testing.T) {
	t.Parallel()
	raw := []byte(
		`{"_id":{"$oid":"0123456789abcdef01234567"},"user_id":17,"pass_key":"summer","created_at":{"$date":"2025-01-01T00:00:00Z"},"total":555,"is_complete":true,"order_details":{"friday":{"lunch":{"type":"no-lunch"}}},"activities":{"cacao":true},"proof_admin":18,"payment_status":"rejected","proof_file":"meal.pdf","proof_received_date":{"$date":"2025-01-02T00:00:00Z"},"payment_rejected_by":19,"payment_rejected_date":{"$date":"2025-01-03T00:00:00Z"},"activities_payment_status":"paid","activities_proof_file":"activity.jpg","activities_payment_confirmed_by":20,"activities_payment_confirmed_date":{"$date":"2025-01-04T00:00:00Z"},"notification_first_sent":true}`,
	)
	p, err := convertFood(raw, 77)
	require.NoError(t, err)
	require.Equal(t, "legacy-food:77:0123456789abcdef01234567", p.ID)
	require.EqualValues(t, 55500, p.Total)
	require.Equal(t, "rejected", p.MealPayment.Status)
	require.Equal(t, "meal.pdf", p.MealPayment.Proof)
	require.EqualValues(t, 19, p.MealPayment.RejectedBy)
	require.Equal(t, "paid", p.ActivityPayment.Status)
	require.Equal(t, "activity.jpg", p.ActivityPayment.Proof)
	require.EqualValues(t, 20, p.ActivityPayment.ConfirmedBy)
	require.Nil(t, p.ActivityPayment.Received, "absence must not fabricate a receipt time")
	require.True(t, p.FirstSent)
	require.False(t, p.LastSent)
}

func TestFoodSourceRejectsUnmappedFacts(t *testing.T) {
	t.Parallel()
	base := map[string]any{"_id": map[string]string{"$oid": "0123456789abcdef01234567"}, "user_id": 17,
		"pass_key": "summer", "created_at": "2025-01-01T00:00:00Z"}
	for name, value := range map[string]any{"notification_first_sent": "true", "activities": map[string]any{"cacao": nil},
		"payment_status": "pending", "activities_proof_received_date": "2025-01-01", "unknown": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fields := maps.Clone(base)
			fields[name] = value
			raw, err := json.Marshal(fields)
			require.NoError(t, err)
			_, err = convertFood(raw, 77)
			require.Error(t, err)
		})
	}
}
