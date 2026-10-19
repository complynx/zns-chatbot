package integration_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func bindingHistoryTask(t *testing.T, f paymentFixture, update int64) db.Task {
	t.Helper()
	task, _, err := f.H.store.Intake(t.Context(), db.IntakeRequest{
		BotID: 123, UpdateID: update, TelegramUserID: 11001, ChatID: 11001,
		Payload: json.RawMessage(`{"kind":"message","text":"Continue specialist access"}`),
	}, nil)
	require.NoError(t, err)
	return task
}

func TestMassageBindingContextBoundsCutoffAndActorDeploymentScope(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	before := bindingHistoryTask(t, f, 99500)
	for range 7 {
		effect := bindingChange(t, f, actor, args)
		args.Active = !effect.Active
		args.ExpectedVersion = effect.Version
	}
	for range 7 {
		_, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
		require.NoError(t, err)
	}
	after := bindingHistoryTask(t, f, 99501)
	has, err := f.H.store.HasMassageBindingContext(t.Context(), actor, before.ID)
	require.NoError(t, err)
	require.False(t, has)
	has, err = f.H.store.HasMassageBindingContext(t.Context(), actor, after.ID)
	require.NoError(t, err)
	require.True(t, has)
	recent, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, after.ID)
	require.NoError(t, err)
	require.Len(t, recent.Pending, 5)
	require.Len(t, recent.Actions, 5)
	for i, item := range recent.Actions {
		require.Equal(t, "completed", item.State)
		require.NotNil(t, item.Result)
		if i > 0 {
			require.False(t, item.Result.UpdatedAt.After(recent.Actions[i-1].Result.UpdatedAt))
		}
	}
	future, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
	require.NoError(t, err)
	unchanged, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, after.ID)
	require.NoError(t, err)
	require.Equal(t, recent, unchanged)
	_, err = f.H.store.ChangeMassageBinding(
		t.Context(),
		actor,
		recent.Pending[0].OperationID,
		recent.Pending[0].ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	late, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, after.ID)
	require.NoError(t, err)
	require.Equal(t, recent.Actions, late.Actions, "A receipt created after the task cutoff is not prior history")
	for _, p := range late.Pending {
		require.NotEqual(t, future.ID, p.ID)
	}
	other := ownerProfileScope(t, f.H, f.OwnerID)
	foreign, err := f.H.store.RecentMassageBindingContext(t.Context(), other, after.ID)
	require.NoError(t, err)
	require.Empty(t, foreign.Pending)
	require.Empty(t, foreign.Actions)
	for _, wrong := range []db.ProfileScope{
		{BotID: actor.BotID + 1, Organization: actor.Organization, Subject: actor.Subject, UserID: actor.UserID},
		{BotID: actor.BotID, Organization: "foreign-org", Subject: actor.Subject, UserID: actor.UserID},
	} {
		_, err = f.H.store.HasMassageBindingContext(t.Context(), wrong, after.ID)
		require.Error(t, err)
		_, err = f.H.store.RecentMassageBindingContext(t.Context(), wrong, after.ID)
		require.Error(t, err)
	}
	missing, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, uuid.New())
	require.NoError(t, err)
	require.Empty(t, missing.Pending)
	require.Empty(t, missing.Actions)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_binding_operations SET expires_at=clock_timestamp()-interval '1 second' WHERE result IS NULL")
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", args.EventID)
	require.NoError(t, err)
	retained, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, after.ID)
	require.NoError(t, err)
	require.Empty(t, retained.Pending)
	require.Equal(t, recent.Actions, retained.Actions)
}

func TestMassageBindingContextReachesExplicitRunAndSteeringAfterRestart(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	p := f.H.peers
	p.mu.Lock()
	p.question = true
	p.adminRole = true
	p.mu.Unlock()
	session := nativeMassageSession(t, f, actor, 11001)
	op := uuid.New()
	prep, err := f.H.store.PrepareMiniAppMassageBinding(t.Context(), actor, session, op, args)
	require.NoError(t, err)
	_, err = f.H.store.AdoptMassageBinding(t.Context(), actor, uuid.New(), prep.ID, args)
	require.NoError(t, err)
	p.mu.Lock()
	require.Empty(t, p.runs)
	p.mu.Unlock()
	f.H.start(t)
	first := paymentReviewMessage(t, f.H, 99600, 11001, "Continue the specialist assignment I prepared in the form")
	f.H.await(t, first, "awaiting_user")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, prep.ID.String())
	require.Contains(t, prompt, "not instructions, consent or a current request")
	require.Contains(t, prompt, "without changing its parameters or renewing expiry")
	require.NotContains(t, prompt, hex.EncodeToString(session.Bearer[:]))
	grant, err := f.H.store.ChangeMiniAppMassageBinding(
		t.Context(),
		actor,
		session,
		op,
		prep.ID,
		args,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	args.Active = false
	args.ExpectedVersion = grant.Version
	next, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
	require.NoError(t, err)
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	p.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	second := paymentReviewMessage(
		t,
		f.H,
		99601,
		11001,
		"I assigned access in the form; continue the pending revocation",
	)
	f.H.await(t, second, "completed")
	f.H.await(t, first, "completed")
	p.mu.Lock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	prompt = p.inputs[0].Text
	p.mu.Unlock()
	require.Contains(t, prompt, next.ID.String())
	require.Contains(t, prompt, op.String())
	require.Contains(t, prompt, `"state":"completed"`)
	require.Contains(t, prompt, "Completed receipts describe original effects, not current specialist access")
	require.NotContains(t, prompt, hex.EncodeToString(session.Bearer[:]))
	recent, err := f.H.store.RecentMassageBindingContext(t.Context(), actor, second)
	require.NoError(t, err)
	require.Len(t, recent.Pending, 1)
	require.Len(t, recent.Actions, 1)
	require.Equal(t, next.ID, recent.Pending[0].ID)
	require.Equal(t, grant, *recent.Actions[0].Result)
}

func TestMassageBindingContextWithholdsPrivateHistoryAfterAdminRoleLoss(t *testing.T) {
	t.Parallel()
	f, actor, args, _ := bindingFixture(t)
	prep, err := f.H.store.PrepareMassageBinding(t.Context(), actor, uuid.New(), args)
	require.NoError(t, err)
	p := f.H.peers
	p.mu.Lock()
	p.adminRole = true
	p.mu.Unlock()
	f.H.start(t)
	first := paymentReviewMessage(t, f.H, 99700, 11001, "Show my pending specialist assignment")
	f.H.await(t, first, "completed")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], prep.ID.String())
	p.adminRole = false
	p.mu.Unlock()
	f.H.iam.Invalidate(actor.Subject)
	second := paymentReviewMessage(t, f.H, 99701, 11001, "What can I do now?")
	f.H.await(t, second, "completed")
	f.H.awaitReplies(t, 2)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 2)
	prompt := p.createdInputs[1]
	p.mu.Unlock()
	require.NotContains(t, prompt, prep.ID.String())
	require.NotContains(t, prompt, "Massage specialist access proposals and original receipts")
	current, err := f.H.store.MassageBindingStatus(t.Context(), actor, nil, uuid.Nil, prep.ID)
	require.NoError(t, err)
	require.Equal(
		t,
		"pending",
		current.State,
		"Role loss withholds the context but never silently applies or replaces a proposal",
	)
}
