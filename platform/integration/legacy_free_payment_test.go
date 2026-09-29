package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func legacyFreeFixture(t *testing.T, received, accepted *time.Time) (*pgxpool.Pool, passbooking.Service) {
	t.Helper()
	db, service := bookingFixture(t)
	_, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='paid',price=0,payment_attempt=NULL WHERE owner='alice';
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record)
 VALUES(repeat('f',64),77,'dance','alice','booking',repeat('e',64),'alice','{}')`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_pass_payment_metadata(event_id,owner,assigned_at,source_key,received_at,accepted_at,proof_reference)
 SELECT event_id,owner,assigned_at,repeat('f',64),$1,$2,'free_pass' FROM core.pass_bookings WHERE owner='alice'`,
		received,
		accepted,
	)
	require.NoError(t, err)
	return db, service
}

func TestLegacyFreePaymentOptionalMetadataAndHTTPAuthorization(t *testing.T) {
	t.Parallel()
	received := time.Date(2025, time.May, 1, 10, 0, 0, 0, time.UTC)
	accepted := received.Add(time.Hour)
	for _, test := range []struct {
		name               string
		received, accepted *time.Time
	}{
		{"absent", nil, nil}, {"received_only", &received, nil},
		{"accepted_only", nil, &accepted}, {"preserved", &received, &accepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, service := legacyFreeFixture(t, test.received, test.accepted)
			signer := identity.Signer{Key: []byte(strings.Repeat("p", 32))}
			handler := api.Handler(
				appservices.NewServices(db, appservices.Options{}),
				signer,
				slog.New(slog.DiscardHandler),
			)
			for _, actor := range []string{"alice", "bob", "visitor"} {
				request := httptest.NewRequest(
					http.MethodGet,
					"/v1/passes/events/dance/participants/alice/payment",
					nil,
				)
				request.Header.Set("Authorization", "Bearer "+signer.Token(actor))
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, request)
				if actor == "visitor" {
					assert.Equal(t, http.StatusForbidden, response.Code)
					continue
				}
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var payment map[string]any
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payment))
				assert.Equal(t, "free", payment["kind"])
				assert.Equal(t, "accepted", payment["decision"])
				assert.Empty(t, payment["attempt"])
				assert.Empty(t, payment["proof_id"])
				assert.Empty(t, payment["receiving_admin"])
				assert.Nil(t, payment["submitter"])
				assert.Nil(t, payment["reviewed_by"])
				assertOptionalPaymentTime(t, test.received, payment["received_at"])
				assertOptionalPaymentTime(t, test.accepted, payment["reviewed_at"])
				_, err := service.PaymentProof(t.Context(), actor, "dance", "alice")
				requireCode(t, err, "forbidden")
			}
			var attempts int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&attempts),
			)
			assert.Zero(t, attempts)
		})
	}
}

func assertOptionalPaymentTime(t *testing.T, expected *time.Time, value any) {
	t.Helper()
	if expected == nil {
		assert.Nil(t, value)
		return
	}
	encoded, ok := value.(string)
	require.True(t, ok)
	parsed, err := time.Parse(time.RFC3339Nano, encoded)
	require.NoError(t, err)
	assert.True(t, expected.Equal(parsed), "source timestamp must retain its instant")
}

func TestLegacyFreePaymentCannotCrossAssignmentOrCurrentReceipt(t *testing.T) {
	t.Parallel()
	db, service := legacyFreeFixture(t, nil, nil)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='assigned',price=100,assigned_at=assigned_at+interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.Payment(t.Context(), "alice", "dance", "alice")
	requireCode(t, err, "forbidden")
	booking, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("current receipt"))
	require.NoError(t, err)
	command := bookingCommand("proof", "native-proof", booking)
	command.ProofID = proof.ID
	_, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	current, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "receipt", current.Kind)
	assert.Equal(t, "pending", current.Decision)
	assert.Equal(t, proof.ID, current.ProofID)
	require.NotNil(t, current.ReceivedAt)
	assert.Nil(t, current.ReviewedAt)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET price=0,payment_attempt=NULL,assigned_at=assigned_at+interval '1 second' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	_, err = service.Payment(t.Context(), "bob", "dance", "alice")
	requireCode(t, err, "forbidden")
	_, err = service.PaymentProof(t.Context(), "alice", "dance", "alice")
	requireCode(t, err, "forbidden")
	history, err := service.PaymentHistoryPage(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	assert.Equal(t, current.Attempt, history.Items[0].Attempt)
}
