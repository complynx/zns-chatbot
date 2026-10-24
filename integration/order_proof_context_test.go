package integration_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func orderProofHistoryTask(t *testing.T, f paymentFixture, update int64) db.Task {
	t.Helper()
	accepted, _, err := f.H.store.Intake(
		t.Context(),
		db.IntakeRequest{
			BotID:          123,
			UpdateID:       update,
			TelegramUserID: 11002,
			ChatID:         11002,
			Payload:        json.RawMessage(`{"kind":"message","text":"Continue my order proof"}`),
		},
		nil,
	)
	require.NoError(t, err)
	return accepted
}

func TestMiniAppOrderProofContextTracksNativeAcceptanceAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	args := orderEvidenceArgs(t, f, order, 99400, "private-native-proof")
	op := uuid.New()
	original, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	before := orderProofHistoryTask(t, f, 99401)
	rows, err := f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, before.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	session := nativeOrderSession(t, f, scope, 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	replay, err := f.H.store.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	require.Equal(t, original, replay)
	after := orderProofHistoryTask(t, f, 99402)
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, before.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, op, rows[0].OperationID)
	require.Equal(t, original.ID, rows[0].EvidenceID)
	require.Equal(t, db.OrderEvidencePending, rows[0].Outcome)
	snapshot := rows
	_, _, confirmed := reviewOrderEvidence(t, f, original, db.OrderEvidenceConfirmed)
	require.Equal(t, db.OrderEvidenceConfirmed, confirmed.State)
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err = reopened.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	require.Equal(t, original, replay)
	later := orderProofHistoryTask(t, f, 99403)
	rows, err = reopened.RecentMiniAppOrderEvidenceActions(t.Context(), scope, later.ID)
	require.NoError(t, err)
	require.Equal(t, snapshot, rows)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, later.ID)
	require.NoError(t, err)
	require.Equal(t, snapshot, rows)
}

func TestMiniAppOrderProofContextIsBoundedAndCurrentOwnerAccessFiltered(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	session := nativeOrderSession(t, f, scope, 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	var operations []uuid.UUID
	for n := range 7 {
		op := uuid.New()
		args := orderEvidenceArgs(t, f, order, int64(99500+n), fmt.Sprintf("private-proof-%d", n))
		claim, err := f.H.store.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
		require.NoError(t, err)
		operations = append(operations, op)
		if n < 6 {
			reviewOrderEvidence(t, f, claim, db.OrderEvidenceRejected)
		}
	}
	task := orderProofHistoryTask(t, f, 99510)
	rows, err := f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, task.ID)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for i, row := range rows {
		require.Equal(t, operations[6-i], row.OperationID)
	}
	raw, err := json.Marshal(rows)
	require.NoError(t, err)
	for _, private := range []string{"amount_minor", "file_id", "private-proof", "reason", "bearer"} {
		require.NotContains(t, string(raw), private)
	}
	foreign := paymentReviewHistoryTask(t, f, 99511)
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, foreign.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	wrong := scope
	wrong.Organization = "foreign-org"
	_, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), wrong, task.ID)
	require.Error(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AN" +
"D user_id=$2", f.Event.ID, f.OwnerID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, task.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registrations SET state='assigned' WHERE event_id=$1 AND " +
"user_id=$2", f.Event.ID, f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1`, f.Event.ID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, task.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	_, err = f.H.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET revoked=true WHERE id=$1`, f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, task.ID)
	require.Error(t, err)
}

func TestMiniAppOrderProofContextJournalFailureRollsBackWholeAdmission(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	args := orderEvidenceArgs(t, f, order, 99600, "atomic-native-proof")
	op := uuid.New()
	session := nativeOrderSession(t, f, scope, 11002)
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	_, err := f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_proof_action() RETURNS trigger LANGUAGE plpgsql AS " +
"$$BEGIN RAISE EXCEPTION 'fixture journal unavailable'; END$$;\n CREATE TR" +
"IGGER fail_proof_action BEFORE INSERT ON shuttle_miniapp_order_evidence_" +
"actions FOR EACH ROW EXECUTE FUNCTION fail_proof_action()")
	require.NoError(t, err)
	_, err = f.H.store.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.Error(t, err)
	var effects int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_order_evidence)+(SELECT count(*) FR" +
"OM shuttle_order_evidence_operations)+(SELECT count(*) FROM shuttle_orde" +
"r_evidence_notices)+(SELECT count(*) FROM shuttle_miniapp_order_evidence" +
"_actions)").
			Scan(&effects),
	)
	require.Zero(t, effects)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_proof_action ON shuttle_miniapp_order_evidence_actions" +
"; DROP FUNCTION fail_proof_action()")
	require.NoError(t, err)
	claim, err := f.H.store.SubmitMiniAppOrderEvidence(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidencePending, claim.State)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	task := orderProofHistoryTask(t, f, 99601)
	rows, err := f.H.store.RecentMiniAppOrderEvidenceActions(t.Context(), scope, task.ID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestMiniAppOrderProofContextReachesOnlyNextExplicitAgentTurn(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	update := telegram.Update{
		ID: 99700,
		Message: &telegram.Message{
			ID:      99700,
			Date:    time.Now().Unix(),
			From:    &telegram.User{ID: 11002, LanguageCode: "en"},
			Chat:    telegram.Chat{ID: 11002, Type: "private"},
			Caption: "Synthetic file only; do not create a payment claim",
			Document: &telegram.MediaFile{
				FileID:   "fixture-source",
				UniqueID: "PRIVATE_NATIVE_PROOF_FILE",
				MIMEType: "application/pdf",
				Filename: "private-proof.pdf",
			},
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	upload, _, err := f.H.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	f.H.await(t, upload.ID, "completed")
	var fileID uuid.UUID
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), `SELECT id FROM shuttle_files WHERE task_id=$1`, upload.ID).
			Scan(&fileID),
	)
	p := f.H.peers
	p.mu.Lock()
	initial := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, 1, initial)
	session := nativeOrderSession(t, f, scope, 11002)
	claim, err := f.H.store.SubmitMiniAppOrderEvidence(
		t.Context(),
		db.EventScope{BotID: scope.BotID, Organization: scope.Organization},
		session,
		uuid.New(),
		db.OrderEvidenceSubmission{
			EventID:              order.EventID,
			OrderID:              order.ID,
			ExpectedOrderVersion: order.Version,
			FileID:               fileID,
			AmountMinor:          order.Snapshot.Quote.TotalMinor,
		},
	)
	require.NoError(t, err)
	p.mu.Lock()
	afterForm := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, initial, afterForm)
	f.H.stop(t)
	f.H.start(t)
	accepted := paymentReviewMessage(t, f.H, 99701, 11002, "Continue the order proof I submitted in the form")
	f.H.await(t, accepted, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[len(p.createdInputs)-1]
	runs := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, initial+1, runs)
	require.Contains(t, prompt, "Recent Mini App order proof submissions accepted before this request")
	require.Contains(t, prompt, claim.ID.String())
	require.Contains(t, prompt, `"outcome":"pending"`)
	require.Contains(t, prompt, "zinusya_order_payments.operation")
	// The original upload turn may contain its file metadata; this historical section may not.
	_, history, ok := strings.Cut(prompt, "Recent Mini App order proof submissions accepted before this request")
	require.True(t, ok)
	section, _, _ := strings.Cut(history, "Continue only the user's current explicit request.")
	for _, private := range []string{claim.FileID.String(), "PRIVATE_NATIVE_PROOF_FILE", "bearer", "amount_minor"} {
		require.NotContains(t, section, private)
	}
}
