package integration_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const pairAssignmentAppPath = "/app/pair-assignments"

type pairAssignmentAppFixture struct {
	pairFixture

	admin   db.PaymentScope
	args    db.PairAssignment
	session appSession
	token   string
}

func setupPairAssignmentApp(t *testing.T) pairAssignmentAppFixture {
	t.Helper()
	f, admin, args := acceptedPairForAssignment(t)
	for _, member := range []struct {
		scope db.PaymentScope
		name  string
	}{{f.A, "Alex Leader"}, {f.B, "Sam Follower"}} {
		owner := db.ProfileScope{
			BotID:        member.scope.BotID,
			Organization: member.scope.Organization,
			UserID:       member.scope.UserID,
			Subject:      member.scope.Subject,
		}
		profile, err := f.H.store.GetProfile(t.Context(), owner)
		require.NoError(t, err)
		_, err = f.H.store.MutateProfile(
			t.Context(),
			owner,
			db.ProfileMutation{
				OperationID:     uuid.New(),
				ExpectedVersion: profile.Version,
				Kind:            "set_name",
				FullName:        member.name,
			},
		)
		require.NoError(t, err)
	}
	f.H.stop(t)
	f.H.peers.mu.Lock()
	f.H.peers.miniAppOrigin = appTestOrigin
	f.H.peers.adminRole = true
	f.H.peers.mu.Unlock()
	f.H.start(t)
	return pairAssignmentAppFixture{pairFixture: f, admin: admin, args: args,
		session: detailsAppSession(t, f.H, 12005, 201), token: pairUserToken(f, admin)}
}

func pairAssignmentAppCall(
	t *testing.T,
	f pairAssignmentAppFixture,
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
	code, raw, _ := appCall(t, f.H, method, pairAssignmentAppPath+path, body, session.Token, appTestOrigin)
	return code, raw
}

func pairAssignmentAppPrepare(t *testing.T, f pairAssignmentAppFixture, operation string) db.PairAssignmentPreparation {
	t.Helper()
	code, raw := pairAssignmentAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/prepare",
		map[string]any{"operation_id": operation, "arguments": f.args},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep db.PairAssignmentPreparation
	require.NoError(t, json.Unmarshal(raw, &prep))
	return prep
}

func TestMiniAppPairAssignmentHTTPNativeAndBothHandoffsRecoverOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := setupPairAssignmentApp(t)
			original := "pair /opaque%2F?#+ operation"
			operation := original
			var prep db.PairAssignmentPreparation
			if path == "agent_to_native" {
				code, raw := adminRequest(
					t,
					f.H,
					f.token,
					http.MethodPost,
					pairAssignAPI+"/prepare",
					map[string]any{"protocol_version": 1, "operation_id": original, "arguments": f.args},
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var agent pairAdoptionPreparation
				require.NoError(t, json.Unmarshal(raw, &agent))
				code, raw = pairAssignmentAppCall(
					t,
					f,
					f.session,
					http.MethodGet,
					"/operations/"+url.PathEscape(original),
					nil,
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var view db.PairAssignmentProposalView
				require.NoError(t, json.Unmarshal(raw, &view))

				require.NotNil(t, view.Leader)
				require.NotNil(t, view.Follower)
				require.Equal(t, "Alex Leader", view.Leader.DisplayName)
				require.Equal(t, "Sam Follower", view.Follower.DisplayName)
				prep = view.Preparation
				require.Equal(t, agent.Preconditions.ProposalID, prep.ID)
				operation = "native-envelope"
				code, raw = pairAssignmentAppCall(
					t,
					f,
					f.session,
					http.MethodPost,
					"/prepare",
					map[string]any{"operation_id": operation, "arguments": f.args, "resume_proposal_id": prep.ID},
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var adopted db.PairAssignmentPreparation
				require.NoError(t, json.Unmarshal(raw, &adopted))
				require.Equal(t, prep, adopted)
			} else {
				prep = pairAssignmentAppPrepare(t, f, original)
				if path == "native_to_agent" {
					operation = "agent-envelope"
					code, raw := pairAdoptionPrepare(t, f.pairFixture, f.token, operation, f.args, prep.ID)
					require.Equal(t, http.StatusOK, code, string(raw))
					var adopted pairAdoptionPreparation
					require.NoError(t, json.Unmarshal(raw, &adopted))
					require.Equal(t, prep.ID, adopted.Preconditions.ProposalID)
					require.Equal(t, prep.Arguments, adopted.Arguments)
					require.True(t, prep.ExpiresAt.Equal(adopted.ExpiresAt))
					code, raw = adminRequest(
						t,
						f.H,
						f.token,
						http.MethodPost,
						pairAssignAPI,
						pairAdoptionWrite(operation, f.args, prep.ID),
					)
					require.Equal(t, http.StatusOK, code, string(raw))
					require.Contains(t, string(raw), `"status":"succeeded"`)
				}
			}
			input := map[string]any{
				"operation_id": operation,
				"arguments":    f.args,
				"proposal_id":  prep.ID,
				"confirmed":    true,
			}
			code, raw := pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusOK, code, string(raw))
			var assigned db.PairAssignmentResult
			require.NoError(t, json.Unmarshal(raw, &assigned))
			require.NotNil(t, assigned.Leader.Pass)
			require.NotNil(t, assigned.Follower.Pass)
			f.H.stop(t)
			f.H.start(t)
			code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusOK, code, string(raw))
			var replay db.PairAssignmentResult
			require.NoError(t, json.Unmarshal(raw, &replay))
			require.Equal(t, assigned, replay)
			for _, key := range []string{original, operation} {
				code, raw = pairAssignmentAppCall(
					t,
					f,
					f.session,
					http.MethodGet,
					"/operations/"+url.PathEscape(key),
					nil,
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var view db.PairAssignmentProposalView
				require.NoError(t, json.Unmarshal(raw, &view))
				require.Equal(t, original, view.OriginalOperationID)
				require.Equal(t, prep, view.Preparation)
				require.Equal(t, db.AssignmentProposalCompleted, view.State)
				require.Equal(t, assigned, *view.Result)
				require.NotNil(t, view.Leader)
				require.NotNil(t, view.Follower)
				require.Equal(t, assigned.Leader.UserID, view.Leader.UserID)
				require.Equal(t, assigned.Follower.UserID, view.Follower.UserID)
				require.Equal(t, "Alex Leader", view.Leader.DisplayName)
				require.Equal(t, "Sam Follower", view.Follower.DisplayName)
				require.Positive(t, view.Leader.TelegramUserID)
				require.Positive(t, view.Follower.TelegramUserID)
			}
			code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/proposals", nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			var pending db.PairAssignmentProposalPage
			require.NoError(t, json.Unmarshal(raw, &pending))
			require.Empty(t, pending.Proposals)
			code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/recent", nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), prep.ID.String())
			var passes, operations int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes),
			)
			require.Equal(t, 2, passes)
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT count(*) FROM shuttle_pair_operations WHERE kind='assign'").
					Scan(&operations),
			)
			require.Equal(t, 1, operations)
		})
	}
}

func TestMiniAppPairAssignmentHTTPRequiresCurrentAuthorityAndProtectsForeignProposals(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	prep := pairAssignmentAppPrepare(t, f, "private-pair")
	foreign := detailsAppSession(t, f.H, 12001, 202)
	code, raw := pairAssignmentAppCall(
		t,
		f,
		foreign,
		http.MethodPost,
		"/prepare",
		map[string]any{"operation_id": "foreign-adoption", "arguments": f.args, "resume_proposal_id": prep.ID},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = pairAssignmentAppCall(
		t,
		f,
		foreign,
		http.MethodPost,
		"/confirm",
		map[string]any{"operation_id": "private-pair", "arguments": f.args, "proposal_id": prep.ID, "confirmed": true},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	for _, path := range []string{"/proposals/" + prep.ID.String(), "/operations/private-pair"} {
		code, raw = pairAssignmentAppCall(t, f, foreign, http.MethodGet, path, nil)
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = false
	f.H.peers.mu.Unlock()
	f.H.iam.Invalidate(f.admin.Subject)
	for _, path := range []string{
		"/events",
		"/proposals",
		"/recent",
		"/events/" + f.EventID.String() + "/candidates",
		"/operations/private-pair",
	} {
		code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, path, nil)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	authority, err := f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"native-event-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.admin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/events", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), f.EventID.String())
	code, raw = pairAssignmentAppCall(t, f, f.session, http.MethodGet, "/events/"+f.EventID.String()+"/candidates", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var candidates struct {
		Candidates []struct {
			Arguments db.PairAssignment `json:"arguments"`
			Leader    struct {
				UserID         uuid.UUID `json:"user_id"`
				TelegramUserID int64     `json:"telegram_user_id"`
				DisplayName    string    `json:"display_name"`
			} `json:"leader"`
			Follower struct {
				UserID uuid.UUID `json:"user_id"`
			} `json:"follower"`
		} `json:"candidates"`
	}
	require.NoError(t, json.Unmarshal(raw, &candidates))
	require.Len(t, candidates.Candidates, 1)
	require.Equal(t, f.args, candidates.Candidates[0].Arguments)
	require.Equal(t, f.A.UserID, candidates.Candidates[0].Leader.UserID)
	require.Equal(t, int64(12002), candidates.Candidates[0].Leader.TelegramUserID)
	require.Equal(t, "Alex Leader", candidates.Candidates[0].Leader.DisplayName)
	require.Equal(t, f.B.UserID, candidates.Candidates[0].Follower.UserID)
	require.NotContains(t, string(raw), "passport_number")
	_, err = f.H.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		"remove-native-event-admin",
		db.AuthorityChange{
			EventID:         f.EventID,
			UserID:          f.admin.UserID,
			Role:            db.AuthorityNone,
			ExpectedVersion: authority.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	code, raw = pairAssignmentAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/confirm",
		map[string]any{"operation_id": "private-pair", "arguments": f.args, "proposal_id": prep.ID, "confirmed": true},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = pairAssignmentAppCall(t, f, appSession{}, http.MethodGet, "/events", nil)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestMiniAppPairAssignmentHTTPStrictJSONAndExplicitConsent(t *testing.T) {
	t.Parallel()
	f := setupPairAssignmentApp(t)
	prep := pairAssignmentAppPrepare(t, f, "strict-pair")
	prepare := map[string]any{"operation_id": "invalid-prepare", "arguments": f.args}
	for _, field := range []string{"confirmed", "proposal_id"} {
		for _, value := range []any{nil, false, prep.ID} {
			prepare[field] = value
			code, raw := pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/prepare", prepare)
			require.Equal(t, http.StatusBadRequest, code, string(raw))
		}
		delete(prepare, field)
	}
	for _, extra := range []map[string]any{
		{},
		{"proposal_id": prep.ID},
		{"confirmed": true},
		{"proposal_id": prep.ID, "confirmed": false},
		{"proposal_id": prep.ID, "confirmed": true, "resume_proposal_id": nil},
	} {
		input := map[string]any{"operation_id": "strict-pair", "arguments": f.args}
		maps.Copy(input, extra)
		code, raw := pairAssignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	valid, _ := json.Marshal(prepare)
	for _, body := range []string{
		string(valid) + ` {}`,
		strings.TrimSuffix(string(valid), "}") + `,"unknown":true}`,
		`{"operation_id":"` + strings.Repeat("x", 70000) + `"}`,
	} {
		code, raw, _ := appCall(
			t,
			f.H,
			http.MethodPost,
			pairAssignmentAppPath+"/prepare",
			body,
			f.session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		pairAssignmentAppPath+"/prepare",
		string(valid),
		f.session.Token,
		"https://other.example",
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestMiniAppPairAssignmentHTTPRevocationDuringEventWaitPreventsEffects(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "iam-invalidation"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := setupPairAssignmentApp(t)
			prep := pairAssignmentAppPrepare(t, f, "wait-pair")
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			blocking, err := f.H.store.Pool().Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocking.Rollback(context.Background()) }()
			_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.EventID)
			require.NoError(t, err)
			type outcome struct {
				code int
				raw  []byte
				err  error
			}
			done := make(chan outcome, 1)
			encoded, err := json.Marshal(
				map[string]any{
					"operation_id": "wait-pair",
					"arguments":    f.args,
					"proposal_id":  prep.ID,
					"confirmed":    true,
				},
			)
			require.NoError(t, err)
			go func() {
				code, raw, _, requestErr := appHTTPCall(
					f.H,
					http.MethodPost,
					pairAssignmentAppPath+"/confirm",
					string(encoded),
					f.session.Token,
					appTestOrigin,
				)
				done <- outcome{code, raw, requestErr}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				readErr := f.H.store.Pool().
					QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND "+
						"pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocking.Conn().PgConn().PID()).
					Scan(&waiting)
				return readErr == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			expected := http.StatusForbidden
			if kind == "logout" {
				code, raw, _ := appCall(
					t,
					f.H,
					http.MethodPost,
					"/app/session/logout",
					`{}`,
					f.session.Token,
					appTestOrigin,
				)
				require.Equal(t, http.StatusNoContent, code, string(raw))
				expected = http.StatusUnauthorized
			} else {
				f.H.iam.Invalidate(f.admin.Subject)
			}
			require.NoError(t, blocking.Rollback(ctx))
			select {
			case result := <-done:
				require.NoError(t, result.err)
				require.Equal(t, expected, result.code, string(result.raw))
			case <-ctx.Done():
				t.Fatal("pair confirmation did not finish")
			}
			var passes int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes),
			)
			require.Zero(t, passes)
		})
	}
}
