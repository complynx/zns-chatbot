package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestNativePassPaymentContextReachesOnlyNextExplicitAgentTurn(t *testing.T) {
	t.Parallel()
	f := setupPayment(t, newPeers(t))
	update := telegram.Update{ID: 98900, Message: &telegram.Message{
		ID:      98900,
		Date:    time.Now().Unix(),
		From:    &telegram.User{ID: 11002, LanguageCode: "en"},
		Chat:    telegram.Chat{ID: 11002, Type: "private"},
		Caption: "A file; do not submit payment yet",
		Document: &telegram.MediaFile{
			FileID:   "fixture-source",
			UniqueID: "native-pass-context-source",
			MIMEType: "application/pdf",
			Filename: "proof.pdf",
		},
	}}
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
	scope := paymentScope(t, f.H, f.OwnerID, false)
	native := nativePaymentSession(t, f, scope, 11002)
	session := db.MiniAppPaymentEvidenceSession{
		UserID:             scope.UserID,
		Bearer:             native.Bearer,
		Generation:         native.Generation,
		AuthorityUnchanged: func() bool { return true },
	}
	p := f.H.peers
	p.mu.Lock()
	initial := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, 1, initial)
	evidence, err := f.H.store.SubmitMiniAppPaymentEvidence(
		t.Context(),
		scope,
		session,
		"native-context-proof",
		f.Event.ID,
		db.MiniAppPaymentEvidenceSubmission{
			PassID:      f.Pass.ID,
			FileID:      fileID,
			AmountMinor: f.Pass.Deposit.RequiredMinor,
			Purpose:     payments.Initial,
		},
	)
	require.NoError(t, err)
	p.mu.Lock()
	require.Len(t, p.runs, initial, "Native submission must not create proactive agent work")
	p.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	next := paymentReviewMessage(t, f.H, 98901, 11002, "Continue the payment proof I sent in the form")
	f.H.await(t, next, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[len(p.createdInputs)-1]
	runs := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, initial+1, runs)
	require.Contains(t, prompt, "Recent native pass payment submissions recorded before this request")
	require.Contains(t, prompt, evidence.ID.String())
	require.Contains(t, prompt, `"state":"pending"`)
	require.Contains(t, prompt, "not confirmed money")
	require.Contains(t, prompt, "refresh zinusya_payments.list")
	require.Contains(t, prompt, "older queued attachment")
}
