package integration_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/miniapp"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const assignmentAppPath = "/app/pass-assignments"

type assignmentAppFixture struct {
	h            *harness
	session      appSession
	adminID      uuid.UUID
	actor, token string
	event        events.Event
	entry        registrations.Registration
}

func setupAssignmentApp(t *testing.T) assignmentAppFixture {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	adminID, token := registrationUser(t, h, 98701, true)
	_, owner := registrationUser(t, h, 98702, false)
	event := registrationEvent(t, h, token)
	entry := registerForEvent(t, h, owner, event.ID, "assignment-app-join", registrations.Leader)
	admin, err := h.store.GetUser(t.Context(), adminID)
	require.NoError(t, err)
	p.mu.Lock()
	p.adminRole = true
	p.mu.Unlock()
	session := detailsAppSession(t, h, 98701, 241)
	require.Equal(t, adminID, session.UserID)
	return assignmentAppFixture{
		h:       h,
		session: session,
		adminID: adminID,
		actor:   admin.Subject,
		token:   token,
		event:   event,
		entry:   entry,
	}
}

func assignmentAppCall(
	t *testing.T,
	f assignmentAppFixture,
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
	code, raw, _ := appCall(t, f.h, method, assignmentAppPath+path, body, session.Token, appTestOrigin)
	return code, raw
}

func assignmentAppInput(f assignmentAppFixture, operation string, proposal uuid.UUID, confirmed bool) map[string]any {
	return map[string]any{"operation_id": operation, "arguments": db.PassAssignment{
		EventID:              f.event.ID,
		RegistrationID:       f.entry.ID,
		ExpectedEventVersion: f.event.Version,
		ExpectedVersion:      f.entry.Version,
	}, "proposal_id": proposal, "confirmed": confirmed}
}

func assignmentAppProposal(t *testing.T, f assignmentAppFixture, operation string) db.AssignmentPreparation {
	t.Helper()
	code, raw := assignmentAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/prepare",
		assignmentAppInput(f, operation, uuid.Nil, false),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep db.AssignmentPreparation
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "Europe/Minsk", prep.Timezone)
	return prep
}

func TestMiniAppAssignmentHTTPNativeAndBothHandoffsRecoverOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := setupAssignmentApp(t)
			original := "original-app-assignment"
			operation := original
			var prep db.AssignmentPreparation
			var agent assignmentProposal
			if path == "agent_to_native" {
				agent = prepareAssignment(t, f.h, f.token, original, f.event, f.entry)
				code, raw := assignmentAppCall(t, f, f.session, http.MethodGet, "/operations/"+original, nil)
				require.Equal(t, http.StatusOK, code, string(raw))
				var view db.AssignmentProposalView
				require.NoError(t, json.Unmarshal(raw, &view))
				require.Equal(t, db.AssignmentProposalPending, view.State)
				prep = view.Preparation
				require.Equal(t, agent.Preconditions.ProposalID, prep.ID)
				operation = "native-app-envelope"
				input := assignmentAppInput(f, operation, uuid.Nil, false)
				input["resume_proposal_id"] = prep.ID
				code, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/prepare", input)
				require.Equal(t, http.StatusOK, code, string(raw))
				var adopted db.AssignmentPreparation
				require.NoError(t, json.Unmarshal(raw, &adopted))
				require.Equal(t, prep, adopted)
			} else {
				prep = assignmentAppProposal(t, f, original)
				if path == "native_to_agent" {
					agent = assignmentProposal{
						OperationID: original,
						Arguments:   prep.Arguments,
						Description: prep.Description,
						ExpiresAt:   prep.ExpiresAt,
					}
					agent.Preconditions.ProposalID = prep.ID
					operation = "agent-app-envelope"
					code, raw := adoptAssignmentHTTP(t, f.h, f.token, operation, agent)
					require.Equal(t, http.StatusOK, code, string(raw))
					require.NoError(t, json.Unmarshal(raw, &agent))
					require.Equal(t, prep.ID, agent.Preconditions.ProposalID)
					require.Equal(t, prep.Arguments, agent.Arguments)
					require.True(t, prep.ExpiresAt.Equal(agent.ExpiresAt))
					code, raw = executeAssignment(t, f.h, f.token, agent)
					require.Equal(t, http.StatusOK, code, string(raw))
					require.Equal(t, "assigned", assignmentResult(t, raw).State)
				}
			}
			code, raw := assignmentAppCall(t, f, f.session, http.MethodGet, "/proposals/"+prep.ID.String(), nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			var before db.AssignmentProposalView
			require.NoError(t, json.Unmarshal(raw, &before))
			require.Equal(t, original, before.OriginalOperationID)
			require.Equal(t, prep, before.Preparation)
			input := assignmentAppInput(f, operation, prep.ID, false)
			code, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusBadRequest, code, string(raw))
			input["confirmed"] = true
			code, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusOK, code, string(raw))
			var assigned registrations.Registration
			require.NoError(t, json.Unmarshal(raw, &assigned))
			require.Equal(t, "assigned", assigned.State)
			require.NotNil(t, assigned.Pass)
			f.h.stop(t)
			f.h.start(t)
			code, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusOK, code, string(raw))
			var replay registrations.Registration
			require.NoError(t, json.Unmarshal(raw, &replay))
			require.Equal(t, assigned, replay)
			for _, key := range []string{original, operation} {
				code, raw = assignmentAppCall(t, f, f.session, http.MethodGet, "/operations/"+key, nil)
				require.Equal(t, http.StatusOK, code, string(raw))
				var view db.AssignmentProposalView
				require.NoError(t, json.Unmarshal(raw, &view))
				require.Equal(t, db.AssignmentProposalCompleted, view.State)
				require.Equal(t, original, view.OriginalOperationID)
				require.Equal(t, assigned, *view.Result)
			}
			code, raw = assignmentAppCall(t, f, f.session, http.MethodGet, "/proposals", nil)
			require.Equal(t, http.StatusOK, code, string(raw))
			var pending db.AssignmentProposalPage
			require.NoError(t, json.Unmarshal(raw, &pending))
			require.Empty(t, pending.Proposals)
			var count int
			require.NoError(
				t,
				f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&count),
			)
			require.Equal(t, 1, count)
			require.NoError(
				t,
				f.h.store.Pool().
					QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", f.actor).
					Scan(&count),
			)
			require.Equal(t, 1, count)
		})
	}
}

func TestMiniAppAssignmentHTTPListsReadableParticipantsAndPages(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	_, err := f.h.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_user_profiles(user_id,version,full_name,bot_language) "+
			"VALUES($1,1,'Readable Participant','en')", f.entry.UserID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_users SET profile='{"first_name":"Telegram name",`+
			`"passport_number":"PRIVATE-98702"}' WHERE id=$1`, f.entry.UserID)
	require.NoError(t, err)
	eventIDs := map[uuid.UUID]bool{f.event.ID: true}
	registrationsByID := map[uuid.UUID]bool{f.entry.ID: true}
	for index := range 20 {
		event := registrationEvent(t, f.h, f.token)
		eventIDs[event.ID] = true
		_, owner := registrationUser(t, f.h, int64(98710+index), false)
		entry := registerForEvent(t, f.h, owner, f.event.ID, fmt.Sprintf("page-join-%d", index), registrations.Follower)
		registrationsByID[entry.ID] = true
	}
	f.h.peers.mu.Lock()
	f.h.peers.adminRole = true
	f.h.peers.mu.Unlock()
	for _, resource := range []string{"events", "registrations"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			path := "/events"
			expected := eventIDs
			if resource == "registrations" {
				path += "/" + f.event.ID.String() + "/registrations"
				expected = registrationsByID
			}
			require.Equal(t, expected, assignmentAppPages(t, f, path))
		})
	}
}

type assignmentAppPage struct {
	Events        []events.Event                    `json:"events"`
	Registrations []db.MiniAppAssignmentParticipant `json:"registrations"`
	Next          *uuid.UUID                        `json:"next"`
}

func assignmentAppPageIDs(t *testing.T, f assignmentAppFixture, page assignmentAppPage) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, len(page.Events)+len(page.Registrations))
	for _, event := range page.Events {
		ids = append(ids, event.ID)
	}
	for _, item := range page.Registrations {
		ids = append(ids, item.Registration.ID)
		if item.Registration.ID == f.entry.ID {
			require.Equal(t, int64(98702), item.TelegramUserID)
			require.Equal(t, "Readable Participant", item.DisplayName)
		}
	}
	return ids
}

func assignmentAppPages(t *testing.T, f assignmentAppFixture, path string) map[uuid.UUID]bool {
	t.Helper()
	seen := make(map[uuid.UUID]bool)
	after := ""
	pages := 0
	for {
		suffix := ""
		if after != "" {
			suffix = "?after=" + after
		}
		code, raw := assignmentAppCall(t, f, f.session, http.MethodGet, path+suffix, nil)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NotContains(t, string(raw), "PRIVATE-98702")
		require.NotContains(t, string(raw), "passport_number")
		require.NotContains(t, string(raw), "legal_name")
		var page assignmentAppPage
		require.NoError(t, json.Unmarshal(raw, &page))
		for _, id := range assignmentAppPageIDs(t, f, page) {
			require.False(t, seen[id])
			seen[id] = true
		}
		pages++
		if page.Next == nil {
			break
		}
		require.Less(t, pages, 3, "Cursor must advance to the next bounded page")
		after = page.Next.String()
	}
	require.Equal(t, 2, pages)
	return seen
}

func TestMiniAppAssignmentHTTPProtectsActorOwnedProposalsAndCurrentRole(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	prep := assignmentAppProposal(t, f, "private-proposal")
	code, raw := assignmentAppCall(t, f, f.session, http.MethodGet, "/proposals", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.AssignmentProposalPage
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Len(t, page.Proposals, 1)
	require.Equal(t, prep.ID, page.Proposals[0].Preparation.ID)
	otherID, _ := registrationUser(t, f.h, 98703, true)
	other := detailsAppSession(t, f.h, 98703, 242)
	require.Equal(t, otherID, other.UserID)
	for _, path := range []string{"/proposals/" + prep.ID.String(), "/operations/private-proposal"} {
		code, raw = assignmentAppCall(t, f, other, http.MethodGet, path, nil)
		require.Equal(t, http.StatusNotFound, code, string(raw))
	}
	code, raw = assignmentAppCall(t, f, other, http.MethodGet, "/proposals", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Empty(t, page.Proposals)
	f.h.peers.mu.Lock()
	f.h.peers.adminRole = false
	f.h.peers.mu.Unlock()
	f.h.iam.Invalidate(f.actor)
	for _, path := range []string{"/events", "/proposals", "/operations/private-proposal"} {
		code, raw = assignmentAppCall(t, f, f.session, http.MethodGet, path, nil)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	code, raw = assignmentAppCall(
		t,
		f,
		f.session,
		http.MethodPost,
		"/confirm",
		assignmentAppInput(f, "private-proposal", prep.ID, true),
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	var passes int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestMiniAppAssignmentHTTPLogoutAndGenerationDenyCompletedReceipt(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"logout", "generation"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f := setupAssignmentApp(t)
			prep := assignmentAppProposal(t, f, "completed-session")
			input := assignmentAppInput(f, "completed-session", prep.ID, true)
			code, raw := assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusOK, code, string(raw))
			bearer := sha256.Sum256([]byte(f.session.Token))
			if failure == "logout" {
				require.NoError(
					t,
					f.h.store.LogoutMiniAppSession(
						t.Context(),
						123,
						bearer,
						miniapp.NewSessionKey("123:test-token").Generation(),
					),
				)
			} else {
				_, err := f.h.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET key_generation=$1 WHERE bearer_hash=$2", make(
						[]byte,
						sha256.Size,
					), bearer[:])
				require.NoError(t, err)
			}
			code, raw = assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
			require.Equal(t, http.StatusUnauthorized, code, string(raw))
			code, raw = assignmentAppCall(t, f, f.session, http.MethodGet, "/operations/completed-session", nil)
			require.Equal(t, http.StatusUnauthorized, code, string(raw))
			var passes int
			require.NoError(
				t,
				f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes),
			)
			require.Equal(t, 1, passes)
		})
	}
}

func TestMiniAppAssignmentHTTPExcludesPairHeldRegistrationsFromSoloTargets(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	partnerID, _ := registrationUser(t, f.h, 98704, false)
	a := paymentScope(t, f.h, f.entry.UserID, false)
	b := paymentScope(t, f.h, partnerID, false)
	invitation, err := f.h.store.CreatePairInvitation(t.Context(), a, "target-list-pair", db.PairInvitationRequest{
		EventID: f.event.ID, TargetUserID: partnerID, ExpectedVersion: f.entry.Version,
	})
	require.NoError(t, err)
	paired, err := f.h.store.DecidePairInvitation(
		t.Context(),
		b,
		"target-list-accept",
		"accept",
		db.PairInvitationDecision{
			EventID:         f.event.ID,
			InvitationID:    invitation.Invitation.ID,
			ExpectedVersion: invitation.Invitation.Version,
			Role:            registrations.Follower,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, paired.Pair)
	f.h.peers.mu.Lock()
	f.h.peers.adminRole = true
	f.h.peers.mu.Unlock()
	code, raw := assignmentAppCall(
		t,
		f,
		f.session,
		http.MethodGet,
		"/events/"+f.event.ID.String()+"/registrations",
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.MiniAppAssignmentRegistrations
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Empty(t, page.Registrations, "Pair-held waitlisted members require a pair assignment, not a solo target")
}

// Each launch has new signed Telegram data as well as a new browser nonce.
func freshAssignmentAppSession(t *testing.T, f assignmentAppFixture) appSession {
	t.Helper()
	fields, err := url.ParseQuery(appProof(98701))
	require.NoError(t, err)
	fields.Set("query_id", uuid.NewString())
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte("123:test-token"))
	mac := hmac.New(sha256.New, key.Sum(nil))
	_, _ = mac.Write(
		[]byte(
			"auth_date=" + fields.Get(
				"auth_date",
			) + "\nquery_id=" + fields.Get(
				"query_id",
			) + "\nuser=" + fields.Get(
				"user",
			),
		),
	)
	fields.Set("hash", hex.EncodeToString(mac.Sum(nil)))
	code, raw, _ := appCall(t, f.h, http.MethodPost, appBootstrapPath, appBody(fields.Encode(), 244), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	return session
}

func assignmentAppRecent(t *testing.T, f assignmentAppFixture, session appSession) db.AssignmentProposalPage {
	t.Helper()
	code, raw := assignmentAppCall(t, f, session, http.MethodGet, "/recent", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var page db.AssignmentProposalPage
	require.NoError(t, json.Unmarshal(raw, &page))
	require.Nil(t, page.Next, "Recent recovery is bounded, with explicit operation lookup for older results")
	return page
}

func TestMiniAppAssignmentHTTPRecentRecoversAfterLostBrowserJournal(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	prep := assignmentAppProposal(t, f, "lost-client-journal")
	for index := range 3 {
		operation := fmt.Sprintf("native-resume-%d", index)
		input := assignmentAppInput(f, operation, uuid.Nil, false)
		input["resume_proposal_id"] = prep.ID
		code, raw := assignmentAppCall(t, f, f.session, http.MethodPost, "/prepare", input)
		require.Equal(t, http.StatusOK, code, string(raw))
		var adopted db.AssignmentPreparation
		require.NoError(t, json.Unmarshal(raw, &adopted))
		require.Equal(t, prep, adopted)
	}
	input := assignmentAppInput(f, "native-resume-2", prep.ID, true)
	code, raw := assignmentAppCall(t, f, f.session, http.MethodPost, "/confirm", input)
	require.Equal(t, http.StatusOK, code, string(raw))
	var assigned registrations.Registration
	require.NoError(t, json.Unmarshal(raw, &assigned))
	require.NotNil(t, assigned.Pass)
	// Simulate losing the local journal and response; the new launch only asks for recent results.
	fresh := freshAssignmentAppSession(t, f)
	require.NotEqual(t, f.session.Token, fresh.Token)
	page := assignmentAppRecent(t, f, fresh)
	require.Len(t, page.Proposals, 1, "Aliases and repeated actions must not duplicate a proposal in recovery")
	view := page.Proposals[0]
	require.Equal(t, "lost-client-journal", view.OriginalOperationID)
	require.Equal(t, prep, view.Preparation)
	require.Equal(t, db.AssignmentProposalCompleted, view.State)
	require.NotNil(t, view.Result)
	require.Equal(t, assigned, *view.Result)
	code, raw = assignmentAppCall(t, f, fresh, http.MethodPost, "/confirm", input)
	require.Equal(t, http.StatusOK, code, string(raw))
	retry := assignmentAppRecent(t, f, fresh)
	require.Len(t, retry.Proposals, 1)
	require.Equal(t, view.Preparation, retry.Proposals[0].Preparation)
	require.Equal(t, view.Result, retry.Proposals[0].Result)
	var passes, receipts int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 1, passes)
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", f.actor).
			Scan(&receipts),
	)
	require.Equal(t, 1, receipts)
	otherID, _ := registrationUser(t, f.h, 98705, true)
	other := detailsAppSession(t, f.h, 98705, 245)
	require.Equal(t, otherID, other.UserID)
	require.Empty(t, assignmentAppRecent(t, f, other).Proposals)
	f.h.peers.mu.Lock()
	f.h.peers.adminRole = false
	f.h.peers.mu.Unlock()
	f.h.iam.Invalidate(f.actor)
	code, raw = assignmentAppCall(t, f, fresh, http.MethodGet, "/recent", nil)
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestMiniAppAssignmentHTTPRecentKeepsLatestBoundedRecovery(t *testing.T) {
	t.Parallel()
	f := setupAssignmentApp(t)
	var oldest, newest db.AssignmentPreparation
	for index := range 21 {
		prep := assignmentAppProposal(t, f, fmt.Sprintf("recent-pending-%02d", index))
		if index == 0 {
			oldest = prep
		}
		newest = prep
	}
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_assignment_preparations SET expires_at=clock_timestamp()-interval "+
			"'1 second' WHERE proposal_id=$1", newest.ID)
	require.NoError(t, err)
	page := assignmentAppRecent(t, f, f.session)
	require.Len(t, page.Proposals, 20)
	require.Equal(t, newest.ID, page.Proposals[0].Preparation.ID)
	seen := make(map[uuid.UUID]bool)
	for _, view := range page.Proposals {
		expectedState := db.AssignmentProposalPending
		if view.Preparation.ID == newest.ID {
			expectedState = db.AssignmentProposalExpired
		}
		require.Equal(t, expectedState, view.State)
		require.NotEqual(t, oldest.ID, view.Preparation.ID)
		require.False(t, seen[view.Preparation.ID])
		seen[view.Preparation.ID] = true
	}
	code, raw := assignmentAppCall(t, f, f.session, http.MethodGet, "/operations/recent-pending-00", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	var recovered db.AssignmentProposalView
	require.NoError(t, json.Unmarshal(raw, &recovered))
	require.Equal(t, oldest, recovered.Preparation)
	require.Equal(t, db.AssignmentProposalPending, recovered.State)
}
