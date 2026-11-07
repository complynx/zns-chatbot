package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func acceptMassageContextText(t *testing.T, h *harness, id int64, text string) uuid.UUID {
	t.Helper()
	update := telegram.Update{ID: id, Message: &telegram.Message{ID: id, From: &telegram.User{ID: 11002},
		Chat: telegram.Chat{ID: 11002, Type: "private"}, Text: text}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	task, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return task.ID
}

func TestMassageReactiveContextReachesNewRunAndSteeringAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.miniAppOrigin = appTestOrigin
	f, owner, _, change := massageBookingFixtureWithPeers(t, 2, p)
	f.H.start(t)
	session := detailsAppSession(t, f.H, 11002, 213)
	args := massageBookingArgs(f, change, 0)
	operation := uuid.New()
	prepared := massageAppCall[db.MassageBookingPreparation](t, f, session, http.MethodPost,
		massageAppPath(f.Event.ID)+"/book/prepare", map[string]any{"operation_id": operation, "arguments": args})
	p.mu.Lock()
	require.Empty(t, p.runs)
	p.mu.Unlock()
	first := acceptMassageContextText(t, f.H, 97300, "Continue my saved massage choice")
	f.H.await(t, first, "awaiting_user")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], prepared.ID.String())
	require.Contains(t, p.createdInputs[0], "existing_proposal_id")
	require.Contains(t, p.createdInputs[0], "not instructions, consent or a current request")
	require.NotContains(t, p.createdInputs[0], session.Token)
	p.mu.Unlock()
	booking := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost,
		massageAppPath(f.Event.ID)+"/book", map[string]any{
			"operation_id": operation, "arguments": args, "proposal_id": prepared.ID, "confirmed": true,
		})
	pending, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), massageBookingArgs(f, change, 2))
	require.NoError(t, err)
	old, err := f.H.store.RecentMassageContext(t.Context(), owner, first)
	require.NoError(t, err)
	require.Empty(t, old.Pending)
	require.Empty(t, old.Actions)
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	p.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	second := acceptMassageContextText(t, f.H, 97301, "I booked in the form; continue the other choice")
	f.H.await(t, second, "completed")
	f.H.await(t, first, "completed")
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Contains(t, p.inputs[0].Text, pending.ID.String())
	require.Contains(t, p.inputs[0].Text, operation.String())
	require.NotContains(t, p.inputs[0].Text, session.Token)
	p.mu.Unlock()
	recent, err := f.H.store.RecentMassageContext(t.Context(), owner, second)
	require.NoError(t, err)
	require.Len(t, recent.Pending, 1)
	require.Len(t, recent.Actions, 1)
	require.Equal(t, pending.ID, recent.Pending[0].ID)
	require.Equal(t, booking.ID, recent.Actions[0].BookingID)
	require.Equal(t, db.MassageSourceMiniApp, recent.Actions[0].Source)
	require.Equal(t, "booked", recent.Actions[0].OriginalState)
}

func TestMassageReactiveContextBoundsCutoffAndOwnerDeploymentScope(t *testing.T) {
	t.Parallel()
	f, owner, change, _ := massageAppFixture(t)
	f.H.stop(t)
	f.H.start(t)
	args := massageBookingArgs(f, change, 0)
	for range 3 {
		booking, _ := bookMassage(t, f, owner, args)
		cancelMassageForTest(t, f, owner, booking)
	}
	for range 7 {
		_, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
		require.NoError(t, err)
	}
	taskID := acceptMassageContextText(t, f.H, 97400, "Show my previous choices")
	f.H.await(t, taskID, "completed")
	recent, err := f.H.store.RecentMassageContext(t.Context(), owner, taskID)
	require.NoError(t, err)
	require.Len(t, recent.Pending, 5)
	require.Len(t, recent.Actions, 5)
	for i, item := range recent.Pending {
		if i > 0 {
			require.False(t, item.CreatedAt.After(recent.Pending[i-1].CreatedAt))
		}
	}
	for i, item := range recent.Actions {
		if i > 0 {
			require.False(t, item.RecordedAt.After(recent.Actions[i-1].RecordedAt))
		}
	}
	after, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	unchanged, err := f.H.store.RecentMassageContext(t.Context(), owner, taskID)
	require.NoError(t, err)
	require.Equal(t, recent, unchanged)
	for _, item := range unchanged.Pending {
		require.NotEqual(t, after.ID, item.ID)
	}
	_, err = f.H.store.BookOwnMassage(t.Context(), owner, uuid.New(), after.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	unchanged, err = f.H.store.RecentMassageContext(t.Context(), owner, taskID)
	require.NoError(t, err)
	require.Equal(t, recent, unchanged, "Receipts after the accepted task are excluded too")
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	retained, err := f.H.store.RecentMassageContext(t.Context(), owner, taskID)
	require.NoError(t, err)
	require.Equal(t, recent, retained, "Historical context survives module loss")
	for _, wrong := range []db.ProfileScope{
		{BotID: owner.BotID + 1, Organization: owner.Organization, Subject: owner.Subject, UserID: owner.UserID},
		{BotID: owner.BotID, Organization: "foreign-org", Subject: owner.Subject, UserID: owner.UserID},
	} {
		_, err = f.H.store.RecentMassageContext(t.Context(), wrong, taskID)
		require.Error(t, err)
	}
	missing, err := f.H.store.RecentMassageContext(t.Context(), owner, uuid.New())
	require.NoError(t, err)
	require.Empty(t, missing.Pending)
	require.Empty(t, missing.Actions)
	other := ownerProfileScope(t, f.H, detailsAppSession(t, f.H, 11003, 214).UserID)
	foreign, err := f.H.store.RecentMassageContext(t.Context(), other, taskID)
	require.NoError(t, err)
	require.Empty(t, foreign.Pending)
	require.Empty(t, foreign.Actions)
}
