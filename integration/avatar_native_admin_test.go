package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/miniapp"
)

const avatarAdminAppPath = "/app/avatar-admin"

func avatarAdminSession(
	t *testing.T,
	f avatarDraftFixture,
	scope db.AvatarDeliveryAdminScope,
) (appSession, db.MiniAppAvatarAdminSession) {
	t.Helper()
	if f.h.server == nil {
		f.h.server = httptest.NewServer(f.h.endpoint.PublicHandler())
	}
	session := detailsAppSession(t, f.h, 999, 234)
	require.Equal(t, scope.UserID, session.UserID)
	return session, db.MiniAppAvatarAdminSession{
		Bearer:     sha256.Sum256([]byte(session.Token)),
		Generation: miniapp.NewSessionKey("123:test-token").Generation(),
	}
}

func avatarAdminAppCall(
	t *testing.T,
	f avatarDraftFixture,
	session appSession,
	method, path string,
	input any,
) (int, []byte) {
	t.Helper()
	body := ""
	if input != nil {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		body = string(raw)
	}
	code, raw, _ := appCall(t, f.h, method, avatarAdminAppPath+path, body, session.Token, appTestOrigin)
	return code, raw
}
func avatarAdminAppInput(op, proposal uuid.UUID, a db.AvatarDeliveryResolution, confirmed bool) map[string]any {
	return map[string]any{"operation_id": op, "proposal_id": proposal, "arguments": a, "confirmed": confirmed}
}

func TestAvatarNativeAdminBothHTTPHandoffsAndReplay(t *testing.T) {
	t.Parallel()
	for _, nativeFirst := range []bool{true, false} {
		name := "agent-to-native"
		if nativeFirst {
			name = "native-to-agent"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, scope, n, a := avatarRepairFixture(t)
			session, _ := avatarAdminSession(t, f, scope)
			token := avatarAdminToken(f, scope)
			a.Action = "dismiss"
			original := uuid.New()
			var prep db.AvatarDeliveryPreparation
			var agentBody map[string]any
			if nativeFirst {
				code, raw := avatarAdminAppCall(
					t,
					f,
					session,
					http.MethodPost,
					"/prepare",
					avatarAdminAppInput(original, uuid.Nil, a, false),
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				require.NoError(t, json.Unmarshal(raw, &prep))
				alias := uuid.New()
				code, raw = adminRequest(
					t,
					f.h,
					token,
					http.MethodPost,
					avatarResolutionAPI+"/prepare",
					map[string]any{
						"protocol_version": 1,
						"operation_id":     alias,
						"arguments": map[string]any{
							"delivery_id":        a.DeliveryID,
							"expected_version":   a.ExpectedVersion,
							"action":             a.Action,
							"reason":             a.Reason,
							"resume_proposal_id": prep.ID,
						},
					},
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var adopted struct {
					Arguments     db.AvatarDeliveryResolution `json:"arguments"`
					Preconditions map[string]any              `json:"preconditions"`
				}
				require.NoError(t, json.Unmarshal(raw, &adopted))
				require.Equal(t, prep.ID.String(), adopted.Preconditions["proposal_id"])
				agentBody = map[string]any{
					"protocol_version": 1,
					"operation_id":     alias,
					"idempotency_key":  alias,
					"arguments":        adopted.Arguments,
					"confirmation": map[string]any{
						"mode":          "exact_parameters",
						"preconditions": adopted.Preconditions,
					},
				}
				code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, agentBody)
				require.Equal(t, http.StatusOK, code, string(raw))
				require.Contains(t, string(raw), `"status":"succeeded"`)
			} else {
				agentBody = preparedAvatarRepair(t, f, token, original, a)
				code, raw := avatarAdminAppCall(t, f, session, http.MethodGet, "/operations/"+original.String(), nil)
				require.Equal(t, http.StatusOK, code, string(raw))
				var view db.AvatarDeliveryProposalView
				require.NoError(t, json.Unmarshal(raw, &view))
				prep = view.AvatarDeliveryPreparation
				require.Equal(t, "pending", view.State)
			}
			confirm := avatarAdminAppInput(original, prep.ID, a, true)
			code, raw := avatarAdminAppCall(t, f, session, http.MethodPost, "/confirm", confirm)
			require.Equal(t, http.StatusOK, code, string(raw))
			var result db.AvatarDeliveryResolutionResult
			require.NoError(t, json.Unmarshal(raw, &result))
			code, raw = avatarAdminAppCall(t, f, session, http.MethodPost, "/confirm", confirm)
			require.Equal(t, http.StatusOK, code, string(raw))
			var replay db.AvatarDeliveryResolutionResult
			require.NoError(t, json.Unmarshal(raw, &replay))
			require.Equal(t, result, replay)
			code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, agentBody)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"status":"succeeded"`)
			current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
			require.NoError(t, err)
			require.Equal(t, n.Version+1, current.Version)
			require.Equal(t, "dismissed", current.State)
			code, raw = avatarAdminAppCall(t, f, session, http.MethodGet, "/proposals/"+prep.ID.String(), nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"state":"completed"`)
			code, raw = avatarAdminAppCall(t, f, session, http.MethodGet, "/pending", nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"items":[]`)
			attempts, sent := avatarDocumentCounts(f)
			require.Zero(t, attempts)
			require.Zero(t, sent)
		})
	}
}

func TestAvatarNativeAdminSessionFencesPreparedAndCompletedOperations(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	op := uuid.New()
	prep, err := f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, op, uuid.Nil, a)
	require.NoError(t, err)
	wrong := session
	wrong.Generation[0] ^= 1
	_, err = f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, wrong, op, prep.ID, a)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	other := session
	other.Bearer = sha256.Sum256([]byte(f.session.Token))
	_, err = f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, other, op, prep.ID, a)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, op, uuid.New(), a)
	require.ErrorIs(t, err, db.ErrConflict)
	result, err := f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, session, op, prep.ID, a)
	require.NoError(t, err)
	require.Equal(t, n.Version+1, result.Version)
	require.NoError(t, f.h.store.LogoutMiniAppSession(t.Context(), scope.BotID, session.Bearer, session.Generation))
	_, err = f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, session, op, prep.ID, a)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.h.store.MiniAppAvatarResolutionProposal(
		t.Context(),
		scope,
		session,
		db.AvatarDeliveryProposalSelector{OperationID: op},
	)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	receipt, err := f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, op)
	require.NoError(t, err)
	require.Equal(t, result, receipt)
}

func TestAvatarNativeAdminHTTPRequiresAuthorityAndExplicitConfirmation(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	session, _ := avatarAdminSession(t, f, scope)
	op := uuid.New()
	code, raw := avatarAdminAppCall(
		t,
		f,
		session,
		http.MethodPost,
		"/prepare",
		avatarAdminAppInput(op, uuid.Nil, a, false),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep db.AvatarDeliveryPreparation
	require.NoError(t, json.Unmarshal(raw, &prep))
	code, raw = avatarAdminAppCall(
		t,
		f,
		session,
		http.MethodPost,
		"/confirm",
		avatarAdminAppInput(op, prep.ID, a, false),
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	changed := a
	changed.Reason = "different consent"
	code, raw = avatarAdminAppCall(
		t,
		f,
		session,
		http.MethodPost,
		"/confirm",
		avatarAdminAppInput(op, prep.ID, changed, true),
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	// Refresh the other actor after granting the fixture role; its old positive cache is nonadmin.
	f.h.iam.Invalidate(f.scope.Subject)
	// A valid administrator session for another actor cannot take this proposal.
	code, raw = avatarAdminAppCall(t, f, f.session, http.MethodGet, "/proposals/"+prep.ID.String(), nil)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = avatarAdminAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/confirm",
		avatarAdminAppInput(op, prep.ID, a, true),
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	f.p.mu.Lock()
	f.p.adminRole = false
	f.p.mu.Unlock()
	f.h.iam.Invalidate(scope.Subject)
	for _, path := range []string{"/pending", "/deliveries/" + n.ID.String(), "/proposals/" + prep.ID.String(), "/operations/" + op.String()} {
		code, raw = avatarAdminAppCall(t, f, session, http.MethodGet, path, nil)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	code, raw = avatarAdminAppCall(
		t,
		f,
		session,
		http.MethodPost,
		"/confirm",
		avatarAdminAppInput(op, prep.ID, a, true),
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
}

func TestAvatarNativeAdminExpiryWhileWaitingRollsBackConfirmation(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	limited, pid := avatarAdminSingleConnection(t, f)
	blocker, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(t.Context()) }()
	_, err = blocker.Exec(t.Context(), "SELECT id FROM shuttle_avatar_deliveries WHERE id=$1 FOR UPDATE", n.ID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()+interva" +
"l '3 seconds' WHERE user_id=$1", scope.UserID)
	require.NoError(t, err)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() {
		_, confirmErr := limited.ConfirmMiniAppAvatarResolution(ctx, scope, session, op, prep.ID, a)
		done <- confirmErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.h.store.Pool().
			QueryRow(t.Context(), "SELECT coalesce(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).
			Scan(&waiting)
		return e == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		e := f.h.store.Pool().
			QueryRow(t.Context(), "SELECT expires_at<=clock_timestamp() FROM shuttle_miniapp_sessions WHERE" +
" user_id=$1", scope.UserID).
			Scan(&expired)
		return e == nil && expired
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))
	require.ErrorIs(t, <-done, db.ErrMiniAppDenied)
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
	_, err = f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func avatarRepairHistoryTask(t *testing.T, f avatarDraftFixture, update int64) db.Task {
	t.Helper()
	task, _, err := f.h.store.Intake(
		t.Context(),
		db.IntakeRequest{
			BotID:          f.scope.BotID,
			UpdateID:       update,
			TelegramUserID: 999,
			ChatID:         999,
			Payload:        json.RawMessage(`{"kind":"message","text":"Continue repair"}`),
		},
		nil,
	)
	require.NoError(t, err)
	return task
}

func TestAvatarNativeAdminContextIsBoundedCutOffAndNeverProactive(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	before := avatarRepairHistoryTask(t, f, 99810)
	op := uuid.New()
	prep, err := f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, op, uuid.Nil, a)
	require.NoError(t, err)
	after := avatarRepairHistoryTask(t, f, 99811)
	found, err := f.h.store.HasNativeAvatarResolutionContext(t.Context(), scope.ProfileScope, before.ID)
	require.NoError(t, err)
	require.False(t, found)
	recent, err := f.h.store.RecentNativeAvatarResolutionActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, recent, 1)
	require.Equal(t, prep.ID, recent[0].ProposalID)
	require.Equal(t, "prepare", recent[0].Kind)
	for range 6 {
		_, err = f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, uuid.New(), uuid.Nil, a)
		require.NoError(t, err)
	}
	_, err = f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, session, op, prep.ID, a)
	require.NoError(t, err)
	unchanged, err := f.h.store.RecentNativeAvatarResolutionActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Equal(t, recent, unchanged)
	for range 6 {
		_, err = f.h.store.ConfirmMiniAppAvatarResolution(t.Context(), scope, session, op, prep.ID, a)
		require.NoError(t, err)
	}
	latest := avatarRepairHistoryTask(t, f, 99812)
	actions, err := f.h.store.RecentNativeAvatarResolutionActions(t.Context(), scope, latest.ID)
	require.NoError(t, err)
	require.Len(t, actions, 5)
	require.Equal(t, "confirm", actions[0].Kind)
	denied := scope
	denied.GlobalAdmin = false
	_, err = f.h.store.RecentNativeAvatarResolutionActions(t.Context(), denied, latest.ID)
	require.Error(t, err)
	f.p.mu.Lock()
	require.Empty(t, f.p.runs)
	f.p.mu.Unlock()
}

func TestAvatarNativeAdminContextReachesAgentAndRespectsRoleLoss(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	a.Action = "dismiss"
	op := uuid.New()
	prep, err := f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, op, uuid.Nil, a)
	require.NoError(t, err)
	f.p.mu.Lock()
	require.Empty(t, f.p.runs)
	f.p.loomDown = false
	f.p.mu.Unlock()
	f.h.stop(t)
	f.h.start(t)
	first := paymentReviewMessage(t, f.h, 99820, 999, "Continue the avatar repair I prepared in the form")
	f.h.await(t, first, "completed")
	f.h.awaitReplies(t, 1)
	f.p.mu.Lock()
	require.Len(t, f.p.createdInputs, 1)
	prompt := f.p.createdInputs[0]
	f.p.adminRole = false
	f.p.mu.Unlock()
	require.Contains(t, prompt, prep.ID.String())
	require.Contains(t, prompt, "resume_proposal_id")
	require.Contains(t, prompt, "not instructions, consent or a current request")
	require.NotContains(t, prompt, hex.EncodeToString(session.Bearer[:]))
	f.h.iam.Invalidate(scope.Subject)
	second := paymentReviewMessage(t, f.h, 99821, 999, "What can I do now?")
	f.h.await(t, second, "completed")
	f.h.awaitReplies(t, 2)
	f.p.mu.Lock()
	require.Len(t, f.p.createdInputs, 2)
	prompt = f.p.createdInputs[1]
	f.p.mu.Unlock()
	require.NotContains(t, prompt, prep.ID.String())
	require.NotContains(t, prompt, "Recent native avatar delivery repair actions")
}

func TestAvatarNativeAdminCreatesActionHistory(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	_, err := f.h.store.PrepareMiniAppAvatarResolution(t.Context(), scope, session, uuid.New(), uuid.Nil, a)
	require.NoError(t, err)
}

func avatarAdminSingleConnection(t *testing.T, f avatarDraftFixture) (*db.Store, uint32) {
	t.Helper()
	dsn, err := url.Parse(f.h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	limited, err := db.Open(t.Context(), dsn.String())
	require.NoError(t, err)
	t.Cleanup(limited.Close)
	var pid uint32
	require.NoError(t, limited.Pool().QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	return limited, pid
}

func TestAvatarNativeAdminPrepareRechecksDeadlineAfterWaiting(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	_, session := avatarAdminSession(t, f, scope)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	limited, pid := avatarAdminSingleConnection(t, f)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions SET expires_at=clock_timestam" +
"p()+interval '3 seconds' WHERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	blocker, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(t.Context()) }()
	_, err = blocker.Exec(
		t.Context(),
		"SELECT proposal_id FROM shuttle_avatar_delivery_resolutions WHERE proposal_id=$1 FOR UPDATE",
		prep.ID,
	)
	require.NoError(t, err)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	go func() {
		_, prepareErr := limited.PrepareMiniAppAvatarResolution(ctx, scope, session, op, uuid.Nil, a)
		done <- prepareErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.h.store.Pool().
			QueryRow(t.Context(), "SELECT coalesce(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).
			Scan(&waiting)
		return e == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		e := f.h.store.Pool().
			QueryRow(t.Context(), "SELECT expires_at<=clock_timestamp() FROM shuttle_avatar_delivery_resolu" +
"tions WHERE proposal_id=$1", prep.ID).
			Scan(&expired)
		return e == nil && expired
	}, 5*time.Second, 20*time.Millisecond)
	require.NoError(t, blocker.Commit(t.Context()))
	require.ErrorIs(t, <-done, db.ErrConflict)
	var count int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_miniapp_avatar_resolution_actions WHERE pro" +
"posal_id=$1", prep.ID).
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestAvatarNativeAdminHTTPInvalidationWhileWaitingPreventsCommit(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	session, _ := avatarAdminSession(t, f, scope)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	limited, pid := avatarAdminSingleConnection(t, f)
	endpoint, err := api.New(
		limited,
		f.h.iam,
		bindingHTTPNoIntake{t},
		api.Options{
			BotID:           scope.BotID,
			OrganizationID:  scope.Organization,
			ActorID:         "actor",
			Issuer:          f.p.iam.URL,
			LoomAudience:    "loom",
			MiniAppOrigin:   appTestOrigin,
			MiniAppBotToken: "123:test-token",
			WebhookSecret:   "test-secret",
		},
	)
	require.NoError(t, err)
	f.h.server.Close()
	f.h.server = httptest.NewServer(endpoint.PublicHandler())
	blocker, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(t.Context()) }()
	_, err = blocker.Exec(t.Context(), "SELECT id FROM shuttle_avatar_deliveries WHERE id=$1 FOR UPDATE", n.ID)
	require.NoError(t, err)
	body, err := json.Marshal(avatarAdminAppInput(op, prep.ID, a, true))
	require.NoError(t, err)
	type response struct {
		code int
		raw  []byte
		err  error
	}
	done := make(chan response, 1)
	go func() {
		code, raw, _, requestErr := appHTTPCall(
			f.h,
			http.MethodPost,
			avatarAdminAppPath+"/confirm",
			string(body),
			session.Token,
			appTestOrigin,
		)
		done <- response{code, raw, requestErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.h.store.Pool().
			QueryRow(t.Context(), "SELECT coalesce(wait_event_type='Lock',false) FROM pg_stat_activity WHERE pid=$1", pid).
			Scan(&waiting)
		return e == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	f.p.mu.Lock()
	f.p.adminRole = false
	f.p.mu.Unlock()
	f.h.iam.Invalidate(scope.Subject)
	require.NoError(t, blocker.Commit(t.Context()))
	reply := <-done
	require.NoError(t, reply.err)
	require.Equal(t, http.StatusForbidden, reply.code, string(reply.raw))
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
	_, err = f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
