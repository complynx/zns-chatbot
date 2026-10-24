package integration_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func historyAccountsFixture(t *testing.T) (*harness, db.ProfileScope, db.HistoryAccountsRequest) {
	t.Helper()
	h := setup(t, newPeers(t))
	operator, _ := registrationUser(t, h, 900, true)
	scope := historyScope(t, h, operator)
	a, b := historyUser(42), historyUser(43)
	a["_id"], b["_id"] = "a", "b"
	a["legal_name"], a["last_name"], a["language_code"] = "Never confirm this", "Telegram hint", "ru"
	raw, digest := historyBundle(t, "account-test", map[string][]map[string]any{
		"users": {
			a,
			b,
		},
		"messages": {historyMessage("a", "Owner42 history", 42), historyMessage("b", "Owner43 history", 43)},
		"files": {
			{
				"_id":               "image",
				"bot_id":            123,
				"user_id":           43,
				"mime_type":         "image/png",
				"tg_file_id":        "synthetic-file",
				"tg_file_unique_id": "synthetic-unique",
			},
		},
	})
	archive := saveHistoryBundle(t, h.store, scope, raw, digest)
	_, err := h.store.ApplyHistory(
		t.Context(),
		scope,
		prepareHistory(t, h.store, scope, archive),
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	request := db.HistoryAccountsRequest{ArchiveID: archive.ArchiveID, SHA256: digest}
	rows, err := h.store.Pool().
		Query(t.Context(), "SELECT id,owner_telegram_id FROM shuttle_legacy_history_records WHERE so" +
"urce_role='users' ORDER BY owner_telegram_id")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var owner int64
		require.NoError(t, rows.Scan(&id, &owner))
		subject := "verified-42"
		if owner == 43 {
			subject = "verified-43"
		}
		request.Mappings = append(request.Mappings, db.HistoryAccountMapping{RecordID: id, Subject: subject})
	}
	require.NoError(t, rows.Err())
	require.Len(t, request.Mappings, 2)
	return h, scope, request
}

func acceptHistoryBindings(context.Context, []db.HistoryAccountBinding) error { return nil }

func historyLiveCounts(t *testing.T, h *harness) []int64 {
	t.Helper()
	counts := make([]int64, 5)
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `SELECT
(SELECT count(*) FROM shuttle_tasks),(SELECT count(*) FROM shuttle_conversations),
(SELECT count(*) FROM shuttle_registrations),(SELECT count(*) FROM shuttle_payment_reviews),
(SELECT count(*) FROM river_job)`).Scan(&counts[0], &counts[1], &counts[2], &counts[3], &counts[4]))
	return counts
}

func TestHistoryAccountsImportReplayReopenAndOwnerReads(t *testing.T) {
	t.Parallel()
	h, scope, request := historyAccountsFixture(t)
	before := historyLiveCounts(t, h)
	checks := 0
	verify := func(_ context.Context, bindings []db.HistoryAccountBinding) error {
		checks++
		require.Equal(
			t,
			[]db.HistoryAccountBinding{
				{BotID: 123, TelegramID: 42, Organization: "org", Subject: "verified-42"},
				{BotID: 123, TelegramID: 43, Organization: "org", Subject: "verified-43"},
			},
			bindings,
		)
		return nil
	}
	receipts, err := h.store.ImportHistoryAccounts(t.Context(), scope, request, verify)
	require.NoError(t, err)
	require.True(t, receipts[0].Created)
	require.True(t, receipts[1].Created)
	require.Equal(t, before, historyLiveCounts(t, h))
	reopened, err := db.Open(t.Context(), h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	replay, err := reopened.ImportHistoryAccounts(t.Context(), scope, request, verify)
	require.NoError(t, err)
	require.Equal(t, receipts, replay)
	require.Equal(t, 2, checks)
	owner := db.ProfileScope{BotID: 123, Organization: "org", Subject: "verified-43", UserID: receipts[1].UserID}
	page, err := reopened.OwnerHistory(t.Context(), owner, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	candidates, err := reopened.OwnerHistoryAvatarCandidates(t.Context(), owner, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, candidates.Items, 1)
	_, err = reopened.OwnerHistoryAvatarSource(
		t.Context(),
		owner,
		candidates.Items[0].RecordID,
		request.ArchiveID,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	stranger := db.ProfileScope{BotID: 123, Organization: "org", Subject: "verified-42", UserID: receipts[0].UserID}
	_, err = reopened.OwnerHistoryAvatarSource(
		t.Context(),
		stranger,
		candidates.Items[0].RecordID,
		request.ArchiveID,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	profile, err := reopened.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	require.Empty(t, profile.FullName)
	require.Equal(t, "auto", profile.BotLanguage)
	user, err := reopened.GetUser(t.Context(), receipts[0].UserID)
	require.NoError(t, err)
	require.Contains(t, string(user.Profile), "Historical name")
	require.NotContains(t, string(user.Profile), "Never confirm")
	require.NotContains(t, string(user.Profile), "untrusted-old-subject")
}

func TestHistoryAccountsPreserveExistingProfileAndRejectChangedBinding(t *testing.T) {
	t.Parallel()
	h, scope, request := historyAccountsFixture(t)
	existing, _ := registrationUser(t, h, 42, false)
	user, err := h.store.GetUser(t.Context(), existing)
	require.NoError(t, err)
	request.Mappings[0].Subject = user.Subject
	owner := historyScope(t, h, existing)
	_, err = h.store.MutateProfile(
		t.Context(),
		owner,
		db.ProfileMutation{OperationID: uuid.New(), Kind: "set_name", FirstName: "Confirmed", LastName: "Person"},
	)
	require.NoError(t, err)
	profile, err := h.store.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	_, err = h.store.MutateProfile(
		t.Context(),
		owner,
		db.ProfileMutation{
			OperationID:     uuid.New(),
			ExpectedVersion: profile.Version,
			Kind:            "set_language",
			BotLanguage:     "en",
		},
	)
	require.NoError(t, err)
	before, err := h.store.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	receipts, err := h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.NoError(t, err)
	require.False(t, receipts[0].Created)
	after, err := h.store.GetProfile(t.Context(), owner)
	require.NoError(t, err)
	after.ObservedAt = before.ObservedAt
	require.Equal(t, before, after)
	afterUser, err := h.store.GetUser(t.Context(), existing)
	require.NoError(t, err)
	require.JSONEq(t, string(user.Profile), string(afterUser.Profile))
	request.Mappings[0].Subject = "replacement-subject"
	_, err = h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestHistoryAccountsBatchRollbackOnConflictAndAuthorityFailure(t *testing.T) {
	t.Parallel()
	h, scope, request := historyAccountsFixture(t)
	denied := errors.New("IAM binding not verified")
	_, err := h.store.ImportHistoryAccounts(
		t.Context(),
		scope,
		request,
		func(context.Context, []db.HistoryAccountBinding) error { return denied },
	)
	require.ErrorIs(t, err, denied)
	var count int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT count(*) FROM shuttle_users WHERE tg_user_id IN(42,43)`).
			Scan(&count),
	)
	require.Zero(t, count)
	existing, _ := registrationUser(t, h, 43, false)
	_, err = h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.ErrorIs(t, err, db.ErrConflict)
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_users WHERE tg_user_id=42`).Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_history_account_receipts`).Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(t, h.store.RevokeUser(t.Context(), existing, true))
	_, err = h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestHistoryAccountsReplayChecksCurrentOwnerAndRejectsNonUserSources(t *testing.T) {
	t.Parallel()
	h, scope, request := historyAccountsFixture(t)
	bad := request
	bad.Mappings = append([]db.HistoryAccountMapping(nil), request.Mappings...)
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT id FROM shuttle_legacy_history_records WHERE source_role='messages' LIMIT 1`).
			Scan(&bad.Mappings[0].RecordID),
	)
	_, err := h.store.ImportHistoryAccounts(t.Context(), scope, bad, acceptHistoryBindings)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	receipts, err := h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.NoError(t, err)
	require.NoError(t, h.store.RevokeUser(t.Context(), receipts[0].UserID, true))
	_, err = h.store.ImportHistoryAccounts(t.Context(), scope, request, acceptHistoryBindings)
	require.ErrorIs(t, err, db.ErrConflict)
	owner := db.ProfileScope{BotID: 123, Organization: "org", Subject: "verified-42", UserID: receipts[0].UserID}
	_, err = h.store.OwnerHistory(t.Context(), owner, uuid.Nil, 20)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
