package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestAgentProfileLatinCompleteNameAndHistoricalSchema(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	userID, token := registrationUser(t, h, 19303, false)
	schemas := profileOutputSchemas(t, h, token)
	pending := changeProfile(t, h, ownerProfileScope(t, h, userID), profileChange("request_name", 0))
	op := uuid.New()
	args := map[string]any{"expected_version": pending.Version, "expectation_id": pending.NameExpectation.ID,
		"first_name": "Анна", "last_name": "Иванова", "first_name_latin": "Anna", "last_name_latin": "Ivanova"}
	code, raw := profileRequest(t, h, token, "complete_name", op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	saved := validProfileReceipt(t, raw, schemas["complete_name"])
	require.Equal(t, "Ivanova", saved.LastNameLatin)
	code, replay := profileRequest(t, h, token, "operation", uuid.New(), map[string]any{"operation_id": op})
	require.Equal(t, http.StatusOK, code, string(replay))
	require.Equal(t, saved, validProfileReceipt(t, replay, schemas["operation"]))
	args = map[string]any{
		"expected_version": saved.Version,
		"first_name":       "Анна",
		"last_name":        "Иванова",
		"first_name_latin": "Anne",
	}
	code, raw = profileRequest(t, h, token, "set_name", uuid.New(), args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var rejected profileAPIReceipt
	require.NoError(t, json.Unmarshal(raw, &rejected))
	require.Equal(t, "rejected", rejected.Status)
}

func TestMiniAppProfileLatinJournalFailureIsAtomicAndRetryable(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, appBody(appProof(456), 81), "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	ctx := t.Context()
	_, err := h.store.Pool().
		Exec(ctx, "CREATE FUNCTION reject_latin_journal_fixture() RETURNS trigger LANGUAGE " +
"plpgsql AS $$ BEGIN RAISE EXCEPTION 'journal unavailable'; END $$;\nCREAT" +
"E TRIGGER reject_latin_journal_fixture BEFORE INSERT ON shuttle_miniapp_" +
"profile_actions FOR EACH ROW EXECUTE FUNCTION reject_latin_journal_fixtu" +
"re()")
	require.NoError(t, err)
	op := uuid.New()
	body, err := json.Marshal(map[string]any{"operation_id": op, "expected_version": 0,
		"first_name": "Анна", "last_name": "Иванова", "first_name_latin": "Anna", "last_name_latin": "Ivanova"})
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	scope := ownerProfileScope(t, h, session.UserID)
	current, err := h.store.GetProfile(ctx, scope)
	require.NoError(t, err)
	require.Zero(t, current.Version)
	require.Empty(t, current.FirstName)
	require.Empty(t, current.FirstNameLatin)
	_, err = h.store.Pool().
		Exec(ctx, "DROP TRIGGER reject_latin_journal_fixture ON shuttle_miniapp_profile_act" +
"ions; DROP FUNCTION reject_latin_journal_fixture()")
	require.NoError(t, err)
	code, raw, _ = appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var saved db.UserProfile
	require.NoError(t, json.Unmarshal(raw, &saved))
	require.Equal(t, "Анна", saved.FirstName)
	require.Equal(t, "Anna", saved.FirstNameLatin)
	code, replay, _ := appCall(t, h, http.MethodPost, "/app/profile/name", string(body), session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.JSONEq(t, string(raw), string(replay))
}
