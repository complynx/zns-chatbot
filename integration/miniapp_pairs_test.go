package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func nativePairSession(t *testing.T, f pairFixture, actor db.PaymentScope, tg int64) db.MiniAppPairSession {
	t.Helper()
	session := nativePaymentSession(t, paymentFixture{H: f.H}, actor, tg)
	return db.MiniAppPairSession{Bearer: session.Bearer, Generation: session.Generation}
}

func TestMiniAppPairsNativeAndMixedPathsShareOriginalReceipts(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := setupPair(t)
			ctx := t.Context()
			a, b := nativePairSession(t, f, f.A, 12002), nativePairSession(t, f, f.B, 12003)
			inviteOp, acceptOp := uuid.NewString(), uuid.NewString()
			if path == "agent_to_native" {
				inviteOp = "agent-invite-opaque-key"
				acceptOp = "native-accept-opaque-key"
			}
			args := db.PairInvitationRequest{
				EventID:         f.EventID,
				TargetUserID:    f.B.UserID,
				ExpectedVersion: f.Entry.Version,
			}
			var invitation db.PairResult
			var err error
			if path == "agent_to_native" {
				invitation, err = f.H.store.CreatePairInvitation(ctx, f.A, inviteOp, args)
			} else {
				invitation, err = f.H.store.CreateMiniAppPairInvitation(ctx, f.A, a, inviteOp, args)
			}
			require.NoError(t, err)
			replay, err := f.H.store.CreatePairInvitation(ctx, f.A, inviteOp, args)
			require.NoError(t, err)
			require.Equal(t, invitation, replay)
			decision := db.PairInvitationDecision{
				EventID:         f.EventID,
				InvitationID:    invitation.Invitation.ID,
				ExpectedVersion: invitation.Invitation.Version,
				Role:            registrations.Follower,
			}
			var accepted db.PairResult
			if path == "native_to_agent" {
				accepted, err = f.H.store.DecidePairInvitation(ctx, f.B, acceptOp, "accept", decision)
			} else {
				accepted, err = f.H.store.DecideMiniAppPairInvitation(ctx, f.B, b, acceptOp, "accept", decision)
			}
			require.NoError(t, err)
			require.NotNil(t, accepted.Pair)
			require.Equal(t, "accepted", accepted.Invitation.State)
			reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			replay, err = reopened.CreateMiniAppPairInvitation(ctx, f.A, a, inviteOp, args)
			require.NoError(t, err)
			require.Equal(t, invitation, replay, "Original receipt does not become current invitation state")
			replay, err = reopened.DecideMiniAppPairInvitation(ctx, f.B, b, acceptOp, "accept", decision)
			require.NoError(t, err)
			require.Equal(t, accepted, replay)
			replay, err = reopened.DecidePairInvitation(ctx, f.B, acceptOp, "accept", decision)
			require.NoError(t, err)
			require.Equal(t, accepted, replay)
			_, err = reopened.DecideMiniAppPairInvitation(
				ctx,
				f.B,
				b,
				acceptOp,
				"decline",
				db.PairInvitationDecision{
					EventID:         f.EventID,
					InvitationID:    invitation.Invitation.ID,
					ExpectedVersion: 1,
				},
			)
			require.ErrorIs(t, err, db.ErrConflict)
			var count int
			require.NoError(t, reopened.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_pairs").Scan(&count))
			require.Equal(t, 1, count)
			f.H.peers.mu.Lock()
			require.Empty(t, f.H.peers.runs, "Native writes and receipts cannot start an agent")
			f.H.peers.mu.Unlock()
		})
	}
}

func TestMiniAppPairsDeclineAndRevokeReleaseTheOriginalHold(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{"decline", "revoke"} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			f := setupPair(t)
			ctx := t.Context()
			invitation := invitePair(t, f)
			actor, tg := f.B, int64(12003)
			if decision == "revoke" {
				actor, tg = f.A, 12002
			}
			session := nativePairSession(t, f, actor, tg)
			op := uuid.NewString()
			args := db.PairInvitationDecision{
				EventID:         f.EventID,
				InvitationID:    invitation.Invitation.ID,
				ExpectedVersion: 1,
			}
			result, err := f.H.store.DecideMiniAppPairInvitation(ctx, actor, session, op, decision, args)
			require.NoError(t, err)
			require.Nil(t, result.Pair)
			want := "declined"
			if decision == "revoke" {
				want = "revoked"
			}
			require.Equal(t, want, result.Invitation.State)
			replay, err := f.H.store.DecidePairInvitation(ctx, actor, op, decision, args)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
			require.NoError(t, err)
			require.Equal(t, f.Entry.RequestedAt, entry.RequestedAt)
			require.Equal(t, f.Entry.Version+2, entry.Version)
			_, err = f.H.store.PrepareAssignment(
				ctx,
				f.A.EventScope,
				"registration-admin",
				uuid.NewString(),
				db.PassAssignment{
					EventID:              f.EventID,
					RegistrationID:       entry.ID,
					ExpectedEventVersion: 1,
					ExpectedVersion:      entry.Version,
				},
			)
			require.NoError(t, err, "Decline/revoke must release the invitation hold")
		})
	}
}

func TestMiniAppPairsDenyWrongExpiredAndRevokedSessionsBeforeReplay(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"wrong_owner", "generation", "expired", "logout", "owner_revoked"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := setupPair(t)
			ctx := t.Context()
			session := nativePairSession(t, f, f.A, 12002)
			op := uuid.NewString()
			args := db.PairInvitationRequest{
				EventID:         f.EventID,
				TargetUserID:    f.B.UserID,
				ExpectedVersion: f.Entry.Version,
			}
			original, err := f.H.store.CreateMiniAppPairInvitation(ctx, f.A, session, op, args)
			require.NoError(t, err)
			switch kind {
			case "wrong_owner":
				session = nativePairSession(t, f, f.B, 12003)
			case "generation":
				session.Generation = sha256.Sum256([]byte("different-key"))
			case "expired":
				_, err = f.H.store.Pool().
					Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 "+
						"second' WHERE bearer_hash=$1", session.Bearer[:])
				require.NoError(t, err)
			case "logout":
				require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, f.A.BotID, session.Bearer, session.Generation))
			case "owner_revoked":
				require.NoError(t, f.H.store.RevokeUser(ctx, f.A.UserID, true))
			}
			_, err = f.H.store.CreateMiniAppPairInvitation(ctx, f.A, session, op, args)
			require.Error(t, err)
			var count int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_pair_operations").Scan(&count),
			)
			require.Equal(t, 1, count)
			var state string
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(ctx, "SELECT state FROM shuttle_pair_invitations WHERE id=$1", original.Invitation.ID).
					Scan(&state),
			)
			require.Equal(t, "pending", state)
		})
	}
}

func TestMiniAppPairsRecheckLogoutAfterWaitingForEventLock(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	ctx := t.Context()
	session := nativePairSession(t, f, f.A, 12002)
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.EventID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.CreateMiniAppPairInvitation(
			ctx,
			f.A,
			session,
			uuid.NewString(),
			db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version},
		)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND "+
				"pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, f.A.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocking.Commit(ctx))
	select {
	case err = <-done:
		require.ErrorIs(t, err, db.ErrMiniAppDenied)
	case <-time.After(5 * time.Second):
		t.Fatal("Invitation did not finish after event lock release")
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_pair_invitations").Scan(&count))
	require.Zero(t, count)
}

func TestMiniAppPairsHTTPConsentPrivacyAndReactiveHistory(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	_, admin := registrationUser(t, h, 12001, true)
	a, token := registrationUser(t, h, 12002, false)
	b, _ := registrationUser(t, h, 12003, false)
	registrationUser(t, h, 12004, false)
	event := registrationEvent(t, h, admin)
	entry := registerForEvent(t, h, token, event.ID, "native-pair-join", registrations.Leader)
	sessions := map[int64]appSession{}
	for index, tg := range []int64{12002, 12003, 12004} {
		code, raw, _ := appCall(
			t,
			h,
			http.MethodPost,
			appBootstrapPath,
			appBody(appProof(tg), byte(151+index)),
			"",
			appTestOrigin,
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		var session appSession
		require.NoError(t, json.Unmarshal(raw, &session))
		sessions[tg] = session
	}
	require.Equal(t, a, sessions[12002].UserID)
	require.Equal(t, b, sessions[12003].UserID)
	code, raw, _ := appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/events/"+event.ID.String()+"/context",
		"",
		sessions[12002].Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var policy db.PairInvitationContext
	require.NoError(t, json.Unmarshal(raw, &policy))
	require.Equal(t, "Europe/Minsk", policy.Timezone)
	op := uuid.NewString()
	input := map[string]any{
		"operation_id": op,
		"arguments":    db.PairInvitationRequest{EventID: event.ID, TargetUserID: b, ExpectedVersion: entry.Version},
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/invite",
		string(body),
		sessions[12002].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var invitation db.PairResult
	require.NoError(t, json.Unmarshal(raw, &invitation))
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/pairs/invitations", "", sessions[12003].Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	var inbox struct {
		Invitations []db.PairInvitationView `json:"invitations"`
	}
	require.NoError(t, json.Unmarshal(raw, &inbox))
	require.Len(t, inbox.Invitations, 1)
	require.Equal(t, registrations.Follower, inbox.Invitations[0].RequiredRole)
	require.NotContains(t, string(raw), "price_minor")
	code, raw, _ = appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/invitations?event_id=invalid&limit=1",
		"",
		sessions[12003].Token,
		"",
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, raw, _ = appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/operations/"+op+"?kind=invite",
		"",
		sessions[12004].Token,
		"",
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/invite",
		string(body),
		sessions[12002].Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	input["user_id"] = b
	forged, err := json.Marshal(input)
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/invite",
		string(forged),
		sessions[12002].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	acceptOp := "agent-accept-opaque-key"
	acceptedBody, err := json.Marshal(
		map[string]any{
			"operation_id": acceptOp,
			"arguments": db.PairInvitationDecision{
				EventID:         event.ID,
				InvitationID:    invitation.Invitation.ID,
				ExpectedVersion: 1,
				Role:            registrations.Follower,
			},
		},
	)
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/accept",
		string(acceptedBody),
		sessions[12004].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	canonical, err := h.store.DecidePairInvitation(
		ctx,
		paymentScope(t, h, b, false),
		acceptOp,
		"accept",
		db.PairInvitationDecision{
			EventID:         event.ID,
			InvitationID:    invitation.Invitation.ID,
			ExpectedVersion: 1,
			Role:            registrations.Follower,
		},
	)
	require.NoError(t, err)
	code, recovered, _ := appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/operations/"+acceptOp+"?kind=accept",
		"",
		sessions[12003].Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(recovered))
	expectedReceipt, err := json.Marshal(canonical)
	require.NoError(t, err)
	require.JSONEq(t, string(expectedReceipt), string(recovered))
	code, first, _ := appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/accept",
		string(acceptedBody),
		sessions[12003].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(first))
	var accepted db.PairResult
	require.NoError(t, json.Unmarshal(first, &accepted))
	require.NotNil(t, accepted.Pair)
	h.stop(t)
	h.start(t)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/accept",
		string(acceptedBody),
		sessions[12003].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	p.mu.Lock()
	require.Empty(t, p.runs)
	p.mu.Unlock()
	update := telegram.Update{
		ID: 98100,
		Message: &telegram.Message{
			ID:   98100,
			From: &telegram.User{ID: 12003, LanguageCode: "en"},
			Chat: telegram.Chat{ID: 12003, Type: "private"},
			Text: "What happened to my pair invitation? Do not change anything.",
		},
	}
	payload, err := json.Marshal(update)
	require.NoError(t, err)
	task, _, err := h.service.Accept(ctx, update, payload)
	require.NoError(t, err)
	h.await(t, task.ID, "completed")
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	prompt := p.createdInputs[0]
	p.mu.Unlock()
	require.Contains(t, prompt, "Recent completed pair invitation and membership operations")
	require.Contains(t, prompt, acceptOp)
	require.NotContains(
		t,
		prompt,
		op,
		"Another participant's operation history is not the current owner's history",
	)
	require.Contains(t, prompt, `"kind":"accept"`)
	require.NotContains(t, prompt, sessions[12003].Token)

	code, raw, _ = appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/my?event_id="+event.ID.String()+"&limit=1",
		"",
		sessions[12003].Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var owned struct {
		Pairs []db.OwnedPair `json:"pairs"`
	}
	require.NoError(t, json.Unmarshal(raw, &owned))
	require.Len(t, owned.Pairs, 1)
	require.Equal(t, accepted.Pair.ID, owned.Pairs[0].Pair.ID)
	dissolveOp := "native-dissolve-opaque-key"
	dissolveBody, err := json.Marshal(
		map[string]any{
			"operation_id": dissolveOp,
			"arguments": db.PairDissolution{
				EventID:         event.ID,
				PairID:          accepted.Pair.ID,
				ExpectedVersion: accepted.Pair.Version,
			},
		},
	)
	require.NoError(t, err)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/dissolve",
		string(dissolveBody),
		sessions[12004].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/dissolve",
		string(dissolveBody),
		sessions[12003].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var dissolved registrations.Pair
	require.NoError(t, json.Unmarshal(raw, &dissolved))
	require.Equal(t, "dissolved", dissolved.State)
	originalDissolution := string(raw)
	h.stop(t)
	h.start(t)
	code, raw, _ = appCall(
		t,
		h,
		http.MethodPost,
		"/app/pairs/dissolve",
		string(dissolveBody),
		sessions[12003].Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, originalDissolution, string(raw))
	code, raw, _ = appCall(
		t,
		h,
		http.MethodGet,
		"/app/pairs/operations/"+dissolveOp+"?kind=dissolve",
		"",
		sessions[12003].Token,
		"",
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, originalDissolution, string(raw))
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/pairs/my", "", sessions[12003].Token, "")
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &owned))
	require.Empty(t, owned.Pairs)
	code, raw, _ = appCall(t, h, http.MethodGet, "/app/pairs/my?limit=0", "", sessions[12003].Token, "")
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1, "Native dissolution cannot create a reactive run")
	p.mu.Unlock()
	update.ID++
	update.Message.ID++
	update.Message.Text = "What happened to my pair? Do not change anything."
	payload, err = json.Marshal(update)
	require.NoError(t, err)
	task, _, err = h.service.Accept(ctx, update, payload)
	require.NoError(t, err)
	h.await(t, task.ID, "completed")
	p.mu.Lock()
	require.Len(t, p.createdInputs, 2)
	prompt = p.createdInputs[1]
	p.mu.Unlock()
	require.Contains(t, prompt, dissolveOp)
	require.Contains(t, prompt, `"kind":"dissolve"`)
	require.Contains(t, prompt, `"pair_id":"`+dissolved.ID.String()+`"`)
	require.Contains(t, prompt, "untrusted history")
}

func TestMiniAppPairsConcurrentEventsDoNotUpgradeOwnerReadLock(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	_, admin := registrationUser(t, f.H, 12005, true)
	second := registrationEvent(t, f.H, admin)
	entry, err := f.H.store.ChangeRegistration(
		ctx,
		f.A.EventScope,
		f.A.Subject,
		"second-event-join",
		db.RegistrationChange{Kind: "register", EventID: second.ID, UserID: f.A.UserID, Role: registrations.Leader},
	)
	require.NoError(t, err)
	sessions := []db.MiniAppPairSession{nativePairSession(t, f, f.A, 12002), nativePairSession(t, f, f.A, 12002)}
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_users WHERE id=$1 FOR SHARE", f.A.UserID)
	require.NoError(t, err)
	done := make(chan error, 2)
	for index, args := range []db.PairInvitationRequest{
		{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version},
		{EventID: second.ID, TargetUserID: f.B.UserID, ExpectedVersion: entry.Version},
	} {
		go func() {
			_, e := f.H.store.CreateMiniAppPairInvitation(ctx, f.A, sessions[index], uuid.NewString(), args)
			done <- e
		}()
	}
	for range 2 {
		select {
		case err = <-done:
			require.NoError(t, err, "Independent event invitations must progress while a profile read holds SHARE")
		case <-ctx.Done():
			t.Fatal("Independent pair operations blocked behind an owner read")
		}
	}
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_pair_invitations WHERE state='pending'").
			Scan(&count),
	)
	require.Equal(t, 2, count)
}
