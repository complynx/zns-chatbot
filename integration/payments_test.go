package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func paymentScope(t *testing.T, h *harness, id uuid.UUID, global bool) db.PaymentScope {
	t.Helper()
	u, err := h.store.GetUser(context.Background(), id)
	require.NoError(t, err)
	deployment := db.EventScope{BotID: 123, Organization: "org"}
	return db.PaymentScope{
		EventScope:  deployment,
		UserID:      id,
		Subject:     u.Subject,
		GlobalAdmin: global,
	}
}
func paymentFile(t *testing.T, h *harness, owner uuid.UUID, update int64, unique string, sentAt time.Time) uuid.UUID {
	t.Helper()
	u, err := h.store.GetUser(context.Background(), owner)
	require.NoError(t, err)
	id := uuid.New()
	_, _, err = h.store.Intake(
		context.Background(),
		db.IntakeRequest{
			BotID:          123,
			UpdateID:       update,
			TelegramUserID: u.TelegramUserID,
			ChatID:         u.TelegramUserID,
			Profile:        json.RawMessage(`{}`),
			Payload:        json.RawMessage(`{"kind":"message","text":"proof fixture"}`),
			Attachments: []files.Incoming{
				{
					ID:         id,
					TelegramID: "fixture-source",
					UniqueID:   unique,
					Kind:       "document",
					MIMEType:   "application/pdf",
					MessageID:  update,
					SentAt:     sentAt,
				},
			},
		}, nil,
	)
	require.NoError(t, err)
	return id
}
func TestPaymentEvidenceNeedsHumanConfirmationAndSupportsTopUp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := setup(t, newPeers(t))
	adminID, admin := registrationUser(t, h, 10001, true)
	ownerID, owner := registrationUser(t, h, 10002, false)
	ambassadorID, _ := registrationUser(t, h, 10003, false)
	event := registrationEvent(t, h, admin)
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 3000, MinimumBPS: 2500}
	code, data := adminRequest(
		t,
		h,
		admin,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{
			"operation_id":     "configure-deposit",
			"expected_version": event.Version,
			"document":         event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	entry := registerForEvent(t, h, owner, event.ID, "join-payments", "leader")
	code, data = adminRequest(
		t,
		h,
		admin,
		http.MethodPost,
		assignmentPath(event.ID, entry.ID),
		assignmentBody("assign-payments", event, entry),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &entry))
	require.NotNil(t, entry.Pass)
	opScope := paymentScope(t, h, adminID, true)
	scope := paymentScope(t, h, ownerID, false)
	ambScope := paymentScope(t, h, ambassadorID, false)
	_, err := h.store.ChangeAuthority(
		ctx,
		opScope.EventScope,
		opScope.Subject,
		"grant-payments",
		db.AuthorityChange{EventID: event.ID, UserID: ambassadorID, Role: db.AuthorityAmbassador},
		uuid.Nil,
	)
	require.NoError(t, err)
	h.stop(t) // Exercise persisted intake/business storage without an agent answering this fixture.
	sent := time.Now().UTC().Truncate(time.Second).Add(-2 * time.Hour)
	due := sent.Add(time.Hour)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_passes SET payment_due_at=$2,assigned_at=$3 WHERE id=$1", entry.Pass.ID, due,
			sent.Add(-time.Hour))
	require.NoError(t, err)
	fileID := paymentFile(t, h, ownerID, 10011, "deposit-proof", sent)
	args := payments.Submission{
		EventID:       event.ID,
		PassID:        entry.Pass.ID,
		FileID:        fileID,
		AmountMinor:   3087,
		Currency:      "BYN",
		CurrencyScale: 2,
		Purpose:       payments.Initial,
	}
	evidence, err := h.store.SubmitPaymentEvidence(ctx, scope, "deposit-evidence", args)
	require.NoError(t, err)
	require.True(t, evidence.Timely)
	require.Equal(t, sent, evidence.SentAt)
	require.True(t, evidence.SubmittedAt.After(due))
	require.Equal(t, payments.Pending, evidence.State)
	balance, err := h.store.PaymentBalance(ctx, scope, event.ID, entry.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
	require.False(t, balance.FullyPaid)
	replay, err := h.store.SubmitPaymentEvidence(ctx, scope, "deposit-evidence", args)
	require.NoError(t, err)
	require.Equal(t, evidence, replay)
	// A fresh upload of the same source cannot fund another expectation.
	duplicate := args
	duplicate.FileID = paymentFile(t, h, ownerID, 10012, "deposit-proof", sent)
	_, err = h.store.SubmitPaymentEvidence(ctx, scope, "duplicate-evidence", duplicate)
	require.ErrorIs(t, err, db.ErrConflict)
	review := payments.ReviewChange{
		EventID:     event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	_, err = h.store.PreparePaymentReview(ctx, scope, "self-confirm", review)
	require.ErrorIs(t, err, payments.ErrDenied)

	// Naming the first late payment a top-up must not bypass the initial deadline.
	prematureArgs := args
	prematureArgs.FileID = paymentFile(t, h, ownerID, 10014, "premature-topup", time.Now().UTC().Truncate(time.Second))
	prematureArgs.Purpose = payments.TopUp
	premature, err := h.store.SubmitPaymentEvidence(ctx, scope, "premature-topup", prematureArgs)
	require.NoError(t, err)
	require.True(t, premature.Timely)
	_, err = h.store.PreparePaymentReview(
		ctx,
		ambScope,
		"premature-confirm",
		payments.ReviewChange{
			EventID:     event.ID,
			EvidenceID:  premature.ID,
			Decision:    payments.Confirmed,
			AmountMinor: 3087,
		},
	)
	require.ErrorIs(t, err, payments.ErrInvalid)
	// Event ending does not prevent confirmation of timely evidence.
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET starts_at=clock_timestamp()-interval '3 days',"+
			"ends_at=clock_timestamp()-interval '1 day' WHERE id=$1", event.ID)
	require.NoError(t, err)
	prep, err := h.store.PreparePaymentReview(ctx, ambScope, "confirm-deposit", review)
	require.NoError(t, err)
	result, err := h.store.ReviewPaymentEvidence(ctx, ambScope, "confirm-deposit", review, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3087, result.Balance.ConfirmedMinor)
	require.EqualValues(t, 9258, result.Balance.RemainingMinor)
	require.True(t, result.Balance.DepositSatisfied)
	require.False(t, result.Balance.FullyPaid)
	again, err := h.store.ReviewPaymentEvidence(ctx, ambScope, "confirm-deposit", review, prep.ID)
	require.NoError(t, err)
	require.Equal(t, result, again)
	// Top-up uses the frozen final deadline, not the elapsed initial window.
	args.FileID = paymentFile(t, h, ownerID, 10013, "topup-proof", time.Now().UTC().Truncate(time.Second))
	args.Purpose = payments.TopUp
	args.AmountMinor = 9258
	topup, err := h.store.SubmitPaymentEvidence(ctx, scope, "topup-evidence", args)
	require.NoError(t, err)
	require.True(t, topup.Timely)
	review = payments.ReviewChange{
		EventID:         event.ID,
		EvidenceID:      topup.ID,
		Decision:        payments.Confirmed,
		AmountMinor:     9258,
		ExpectedVersion: 1,
	}
	prep, err = h.store.PreparePaymentReview(ctx, ambScope, "confirm-topup", review)
	require.NoError(t, err)
	result, err = h.store.ReviewPaymentEvidence(ctx, ambScope, "confirm-topup", review, prep.ID)
	require.NoError(t, err)
	require.True(t, result.Balance.FullyPaid)
	require.Zero(t, result.Balance.RemainingMinor)
	// Scoped proof sharing does not grant the general file API foreign-owner access.
	_, err = h.store.File(
		ctx,
		db.EventContextScope{
			BotID:        ambScope.BotID,
			Organization: ambScope.Organization,
			UserID:       ambScope.UserID,
			Subject:      ambScope.Subject,
		},
		fileID,
	)
	require.Error(t, err)
	proofFile, err := h.store.PaymentEvidenceFile(ctx, ambScope, event.ID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, fileID, proofFile.ID)
	_, err = h.store.ChangeAuthority(
		ctx,
		opScope.EventScope,
		opScope.Subject,
		"revoke-payments",
		db.AuthorityChange{EventID: event.ID, UserID: ambassadorID, Role: db.AuthorityNone, ExpectedVersion: 1},
		uuid.Nil,
	)
	require.NoError(t, err)
	_, err = h.store.PaymentEvidenceFile(ctx, ambScope, event.ID, evidence.ID)
	require.ErrorIs(t, err, payments.ErrDenied)
}
