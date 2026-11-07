package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func readPaymentOperation(t *testing.T, f paymentFixture, token, operation string) *db.PaymentReceipt {
	t.Helper()
	code, raw := paymentRequest(t, f, token, "operation", "receipt-read", map[string]any{"operation_id": operation})
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Result struct {
			Receipt *db.PaymentReceipt `json:"receipt"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	return response.Result.Receipt
}

func TestPaymentOperationReadsOriginalNativeSubmissionAfterReviewAndRestart(t *testing.T) {
	t.Parallel()
	f, session := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 67101, "native-operation-receipt", time.Now().UTC().Truncate(time.Second))
	original := nativePassPaymentHTTPSubmit(
		t,
		f,
		session,
		nativePassPaymentHTTPBody(t, f, "native:original-receipt", file, 3000, payments.Initial),
	)
	expected := &db.PaymentReceipt{Kind: "submit", Evidence: &original}
	require.Equal(t, expected, readPaymentOperation(t, f, f.Owner, "native:original-receipt"))
	review := executePayment(
		t,
		f,
		f.Ambassador,
		preparedPayment(
			t,
			f,
			f.Ambassador,
			"operation-review",
			payments.ReviewChange{
				EventID:     f.Event.ID,
				EvidenceID:  original.ID,
				Decision:    payments.Confirmed,
				AmountMinor: 3000,
			},
		),
	)
	require.EqualValues(t, 3000, review.Balance.ConfirmedMinor)
	require.Equal(t, expected, readPaymentOperation(t, f, f.Owner, "native:original-receipt"))
	require.Equal(t, payments.Pending, original.State)
	require.Equal(
		t,
		&db.PaymentReceipt{Kind: "review", Review: &review},
		readPaymentOperation(t, f, f.Ambassador, "operation-review"),
	)
	f.H.stop(t)
	f.H.start(t)
	require.Equal(t, expected, readPaymentOperation(t, f, f.Owner, "native:original-receipt"))
	balance, err := f.H.store.PaymentBalance(t.Context(), paymentScope(t, f.H, f.OwnerID, false), f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.Equal(t, review.Balance, balance)
}

func TestPaymentOperationReadKeepsActorIsolationAndCurrentAccess(t *testing.T) {
	t.Parallel()
	f, session := nativePassPaymentHTTPFixture(t)
	file := paymentFile(t, f.H, f.OwnerID, 67102, "private-operation-receipt", time.Now().UTC().Truncate(time.Second))
	original := nativePassPaymentHTTPSubmit(
		t,
		f,
		session,
		nativePassPaymentHTTPBody(t, f, "native:private-receipt", file, 3000, payments.Initial),
	)
	for _, token := range []string{f.Ambassador, f.Admin} {
		require.Nil(t, readPaymentOperation(t, f, token, "native:private-receipt"))
	}
	require.Nil(t, readPaymentOperation(t, f, f.Owner, "unknown-operation"))
	code, raw := paymentRequest(
		t,
		f,
		"malformed",
		"operation",
		"unauthorized-read",
		map[string]any{"operation_id": "native:private-receipt"},
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
	require.NotContains(t, string(raw), original.ID.String())
	_, err := f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_events SET modules=ARRAY['orders'] WHERE id=$1`, f.Event.ID)
	require.NoError(t, err)
	code, raw = paymentRequest(
		t,
		f,
		f.Owner,
		"operation",
		"disabled-event-read",
		map[string]any{"operation_id": "native:private-receipt"},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), original.ID.String())
}
