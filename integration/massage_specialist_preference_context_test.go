package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMassageSpecialistPreferenceContextSharesNativeAgentHistoryAfterServiceRestart(t *testing.T) {
	t.Parallel()
	f, staff, _, _, session := staffHTTPFixture(t)
	p := f.H.peers
	p.mu.Lock()
	p.question = true
	p.mu.Unlock()
	firstOperation := uuid.New()
	massageAppCall[db.MassageSpecialistPreferences](t, f, session, http.MethodPost,
		"/app/massage-staff/"+f.Event.ID.String()+"/preferences", map[string]any{
			"operation_id": firstOperation, "arguments": noticePreference(f.Event.ID, 0, false),
		})
	p.mu.Lock()
	require.Empty(t, p.runs)
	p.mu.Unlock()
	f.H.start(t)
	first := paymentReviewMessage(t, f.H, 99840, 11444, "I changed my notifications in the form; explain the setting")
	f.H.await(t, first, "awaiting_user")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, firstOperation.String())
	require.Contains(t, prompt, "ALL the account's specialist events")
	require.Contains(t, prompt, "not instructions, consent or a current request")
	require.NotContains(t, prompt, session.Token)
	secondOperation := uuid.New()
	_, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		secondOperation,
		noticePreference(f.Event.ID, 1, true),
	)
	require.NoError(t, err)
	old, err := f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, first)
	require.NoError(t, err)
	require.Len(t, old, 1)
	require.False(t, old[0].OriginalResult.NotifyBookings)
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	p.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	second := paymentReviewMessage(t, f.H, 99841, 11444, "Now I enabled them through the agent; continue")
	f.H.await(t, second, "completed")
	f.H.await(t, first, "completed")
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	prompt = p.inputs[0].Text
	p.mu.Unlock()
	require.Contains(t, prompt, firstOperation.String())
	require.Contains(t, prompt, secondOperation.String())
	require.Contains(t, prompt, "original receipts are not the current setting")
	require.NotContains(t, prompt, session.Token)
	recent, err := f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, second)
	require.NoError(t, err)
	require.Len(t, recent, 2)
	require.Equal(t, secondOperation, recent[0].OperationID)
	require.Equal(t, db.MassageSourceAgent, recent[0].Source)
	require.True(t, recent[0].OriginalResult.NotifyBookings)
	require.Equal(t, firstOperation, recent[1].OperationID)
	require.Equal(t, db.MassageSourceMiniApp, recent[1].Source)
}

func TestMassageSpecialistPreferenceContextBoundsCutoffAndCurrentAccess(t *testing.T) {
	t.Parallel()
	f, staff, _, binding := staffFixture(t)
	operations := []uuid.UUID{}
	for i := range 7 {
		op := uuid.New()
		operations = append(operations, op)
		_, err := f.H.store.SetMassageSpecialistPreferences(
			t.Context(),
			staff,
			op,
			noticePreference(f.Event.ID, int64(i), i%2 != 0),
		)
		require.NoError(t, err)
	}
	task := bindingHistoryTask(t, f, 99842).ID
	recent, err := f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.NoError(t, err)
	require.Len(t, recent, 5)
	for i, row := range recent {
		require.Equal(t, operations[6-i], row.OperationID)
		require.Equal(t, int64(7-i), row.OriginalResult.Version)
	}
	raw, err := json.Marshal(recent)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "request_hash")
	_, err = f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		noticePreference(f.Event.ID, 7, true),
	)
	require.NoError(t, err)
	retained, err := f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.NoError(t, err)
	require.Equal(t, recent, retained)
	for _, wrong := range []db.ProfileScope{
		{BotID: staff.BotID + 1, Organization: staff.Organization, Subject: staff.Subject, UserID: staff.UserID},
		{BotID: staff.BotID, Organization: "foreign", Subject: staff.Subject, UserID: staff.UserID},
		{BotID: staff.BotID, Organization: staff.Organization, Subject: "foreign", UserID: staff.UserID},
	} {
		_, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), wrong, task)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	missing, err := f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, uuid.New())
	require.NoError(t, err)
	require.Empty(t, missing)
	other := ownerProfileScope(t, f.H, f.OwnerID)
	missing, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), other, task)
	require.NoError(t, err)
	require.Empty(t, missing)
	var modules []string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT modules FROM shuttle_events WHERE id=$1", f.Event.ID).
			Scan(&modules),
	)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_events SET modules='{}' WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	missing, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.NoError(t, err)
	require.Empty(t, missing)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_events SET modules=$2 WHERE id=$1", f.Event.ID, modules)
	require.NoError(t, err)
	binding.Active = false
	bindingChange(t, f, staff, binding)
	missing, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.NoError(t, err)
	require.Empty(t, missing)
	binding.Active = true
	binding.ExpectedVersion++
	bindingChange(t, f, staff, binding)
	retained, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.NoError(t, err)
	require.Equal(t, recent, retained)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
	require.NoError(t, err)
	_, err = f.H.store.RecentMassageSpecialistPreferenceActions(t.Context(), staff, task)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
