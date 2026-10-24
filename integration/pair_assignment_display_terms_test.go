package integration_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestPairAssignmentDisplayTermsRemainOriginalAfterRestartAndEventChange(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	event, err := f.H.store.Event(t.Context(), f.admin.EventScope, f.args.EventID)
	require.NoError(t, err)
	code, raw := pairAssignmentAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/prepare",
		map[string]any{"operation_id": "display-terms", "arguments": f.args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var original struct {
		DisplayTerms *struct {
			EventName            string    `json:"event_name"`
			PaymentWindowSeconds int32     `json:"payment_window_seconds"`
			PaymentClosesAt      time.Time `json:"payment_closes_at"`
		} `json:"display_terms"`
	}
	require.NoError(t, json.Unmarshal(raw, &original))
	require.NotNil(t, original.DisplayTerms)
	require.Equal(t, event.Document.Name, original.DisplayTerms.EventName)
	require.Equal(t, event.Document.Registration.PaymentWindowSeconds, original.DisplayTerms.PaymentWindowSeconds)
	require.True(t, event.Document.Registration.ClosesAt.Equal(original.DisplayTerms.PaymentClosesAt))
	var prepared db.PairAssignmentPreparation
	require.NoError(t, json.Unmarshal(raw, &prepared))
	event.Document.Name = "Changed event"
	event.Document.Registration.PaymentWindowSeconds++
	_, err = f.H.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"change-display-event",
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	f.H.stop(t)
	f.H.start(t)
	code, raw = pairAssignmentAppCall(
		t,
		f,
		f.session,
		http.MethodGet,
		"/operations/"+url.PathEscape("display-terms"),
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var view struct {
		Preparation json.RawMessage `json:"preparation"`
	}
	require.NoError(t, json.Unmarshal(raw, &view))
	var recovered db.PairAssignmentPreparation
	require.NoError(t, json.Unmarshal(view.Preparation, &recovered))
	require.Equal(t, prepared, recovered)
	var snapshot struct {
		DisplayTerms json.RawMessage `json:"display_terms"`
	}
	require.NoError(t, json.Unmarshal(view.Preparation, &snapshot))
	var originalSnapshot struct {
		DisplayTerms json.RawMessage `json:"display_terms"`
	}
	originalRaw, err := json.Marshal(original)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(originalRaw, &originalSnapshot))
	require.JSONEq(t, string(originalSnapshot.DisplayTerms), string(snapshot.DisplayTerms))
	requireNativePairAssignmentEffects(t, f.pairFixture, f.admin, 0, 0, 1)
}

func TestPairAssignmentLegacyDescriptionSurvivesRestartAndConfirmation(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	prep := pairAssignmentAppPrepare(t, f, "legacy-display")
	// Reproduce the persisted shape written before structured display terms existed.
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_pair_preparations SET document=document-'display_terms' W" +
"HERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	prep.DisplayTerms = nil
	f.H.stop(t)
	f.H.start(t)
	code, raw := pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/operations/legacy-display", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var view db.PairAssignmentProposalView
	require.NoError(t, json.Unmarshal(raw, &view))
	require.Equal(t, prep, view.Preparation)
	code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", map[string]any{
		"operation_id": "legacy-display", "arguments": f.args, "proposal_id": prep.ID, "confirmed": true,
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/operations/legacy-display", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &view))
	require.Equal(t, "completed", string(view.State))
	require.Equal(t, prep, view.Preparation)
	var storedHasTerms bool
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT document ? 'display_terms' FROM shuttle_pair_preparations WHERE p" +
"roposal_id=$1", prep.ID).
			Scan(&storedHasTerms),
	)
	require.False(t, storedHasTerms)
	requireNativePairAssignmentEffects(t, f.pairFixture, f.admin, 2, 1, 2)
}
