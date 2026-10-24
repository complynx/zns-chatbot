package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const authorityPath = "/internal/agent/administration/authority"

func prepareAuthority(t *testing.T, h *harness, token, op string, args db.AuthorityChange) map[string]any {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
	code, data := adminRequest(t, h, token, http.MethodPost, authorityPath+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(data))
	var prep struct {
		Status        string         `json:"status"`
		Preconditions map[string]any `json:"preconditions"`
		Description   string         `json:"description"`
	}
	require.NoError(t, json.Unmarshal(data, &prep))
	require.Equal(t, "prepared", prep.Status)
	require.Contains(t, prep.Description, args.UserID.String())
	require.Contains(t, prep.Description, args.Role)
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}
func ownAuthority(t *testing.T, h *harness, token string, event uuid.UUID) db.EventAuthority {
	t.Helper()
	code, data := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/authorities/me",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "own-role",
			"arguments":        map[string]any{"event_id": event},
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var result struct {
		Result db.EventAuthority `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	return result.Result
}
func TestEventAuthorityGrantRevokeReplayAndRestart(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9951, true)
	user, token := registrationUser(t, h, 9952, false)
	event := registrationEvent(t, h, operator)
	require.Equal(t, "none", ownAuthority(t, h, token, event.ID).Role)
	args := db.AuthorityChange{EventID: event.ID, UserID: user, Role: "ambassador"}
	body := prepareAuthority(t, h, operator, "grant-ambassador", args)
	require.EqualValues(t, 0, ownAuthority(t, h, token, event.ID).Version) // Preparation grants nothing.
	code, data := adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var first struct {
		Status string            `json:"status"`
		Result db.EventAuthority `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &first))
	require.Equal(t, "succeeded", first.Status)
	require.Equal(t, "ambassador", first.Result.Role)
	require.Equal(t, first.Result, ownAuthority(t, h, token, event.ID))
	// A scoped ambassador cannot grant themselves or anyone else roles.
	code, _ = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		authorityPath+"/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "escalate",
			"arguments": db.AuthorityChange{
				EventID:         event.ID,
				UserID:          user,
				Role:            "administrator",
				ExpectedVersion: 1,
			},
		},
	)
	require.Equal(t, http.StatusForbidden, code)
	otherEvent := registrationEvent(t, h, operator)
	require.Equal(t, "none", ownAuthority(t, h, token, otherEvent.ID).Role)
	revoke := prepareAuthority(
		t,
		h,
		operator,
		"revoke-ambassador",
		db.AuthorityChange{EventID: event.ID, UserID: user, Role: "none", ExpectedVersion: 1},
	)
	code, data = adminRequest(t, h, operator, http.MethodPost, authorityPath, revoke)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, "none", ownAuthority(t, h, token, event.ID).Role)
	h.stop(t)
	h.start(t)
	code, data = adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var replay struct {
		Result db.EventAuthority `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, first.Result, replay.Result)
	// Replaying the receipt must never restore the old role.
	current := ownAuthority(t, h, token, event.ID)
	require.Equal(t, "none", current.Role)
	require.EqualValues(t, 2, current.Version)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		authorityPath+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "grant-ambassador"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, first.Result, replay.Result)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/agent/administration/authorities",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "list-roles",
			"arguments":        map[string]any{"event_id": event.ID},
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), user.String())
	require.Contains(t, string(data), `"role":"none"`)
}
func TestEventAuthorityRejectsStaleChangedAndForeignRequests(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9961, true)
	user, token := registrationUser(t, h, 9962, false)
	event := registrationEvent(t, h, operator)
	args := db.AuthorityChange{EventID: event.ID, UserID: user, Role: "ambassador"}
	body := prepareAuthority(t, h, operator, "grant", args)
	wrong := db.AuthorityChange{EventID: event.ID, UserID: user, Role: "administrator"}
	body["arguments"] = wrong
	code, data := adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.Equal(t, "none", ownAuthority(t, h, token, event.ID).Role)
	body["arguments"] = args
	code, data = adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"succeeded"`)
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		authorityPath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": "stale", "arguments": args},
	)
	require.Equal(t, http.StatusConflict, code)
	foreign := uuid.New()
	_, err := h.store.Pool().
		Exec(context.Background(), "INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile,zitadel_subject,z" +
"itadel_org) VALUES($1,124,9963,'{}','foreign-authority','org')", foreign)
	require.NoError(t, err)
	args.UserID = foreign
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		authorityPath+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": "foreign-user", "arguments": args},
	)
	require.Equal(t, http.StatusNotFound, code)
	args.UserID = user
	args.ExpectedVersion = 1
	args.Role = "none"
	revoke := prepareAuthority(t, h, operator, "after-revocation", args)
	_, err = h.store.Pool().Exec(context.Background(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", user)
	require.NoError(t, err)
	code, data = adminRequest(t, h, operator, http.MethodPost, authorityPath, revoke)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	code, _ = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/agent/authorities/me",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "denied",
			"arguments":        map[string]any{"event_id": event.ID},
		},
	)
	require.Equal(t, http.StatusForbidden, code)
}
func TestConcurrentEventAuthorityChangesCommitOneVersion(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9971, true)
	user, token := registrationUser(t, h, 9972, false)
	event := registrationEvent(t, h, operator)
	a := prepareAuthority(
		t,
		h,
		operator,
		"concurrent-a",
		db.AuthorityChange{EventID: event.ID, UserID: user, Role: "ambassador"},
	)
	b := prepareAuthority(
		t,
		h,
		operator,
		"concurrent-b",
		db.AuthorityChange{EventID: event.ID, UserID: user, Role: "administrator"},
	)
	var wg sync.WaitGroup
	results := make(chan string, 2)
	for _, body := range []map[string]any{a, b} {
		wg.Go(func() {
			code, data := adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
			require.Equal(t, http.StatusOK, code, string(data))
			var r struct {
				Status string `json:"status"`
			}
			require.NoError(t, json.Unmarshal(data, &r))
			results <- r.Status
		})
	}
	wg.Wait()
	close(results)
	states := []string{}
	for r := range results {
		states = append(states, r)
	}
	require.ElementsMatch(t, []string{"succeeded", "rejected"}, states)
	require.EqualValues(t, 1, ownAuthority(t, h, token, event.ID).Version)
}

func TestExpiredAuthorityApprovalHasNoEffectAndActorCannotBorrowProposal(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9981, true)
	_, otherAdmin := registrationUser(t, h, 9982, true)
	user, token := registrationUser(t, h, 9983, false)
	event := registrationEvent(t, h, operator)
	args := db.AuthorityChange{EventID: event.ID, UserID: user, Role: "ambassador"}
	body := prepareAuthority(t, h, operator, "expiring-role", args)
	code, data := adminRequest(t, h, otherAdmin, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	_, err := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_authority_preparations SET expires_at=clock_timestamp()-i" +
"nterval '1 second' WHERE operation_id='expiring-role'")
	require.NoError(t, err)
	code, data = adminRequest(t, h, operator, http.MethodPost, authorityPath, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.EqualValues(t, 0, ownAuthority(t, h, token, event.ID).Version)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		authorityPath+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "expiring-role"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"not_found"`)
}
