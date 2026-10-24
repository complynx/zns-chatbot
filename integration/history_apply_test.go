package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func historyScope(t *testing.T, h *harness, id uuid.UUID) db.ProfileScope {
	t.Helper()
	u, err := h.store.GetUser(context.Background(), id)
	require.NoError(t, err)
	return db.ProfileScope{BotID: 123, Organization: "org", Subject: u.Subject, UserID: id}
}

func historyBundle(t *testing.T, source string, collections map[string][]map[string]any) ([]byte, string) {
	t.Helper()
	root := t.TempDir()
	manifest := legacyimport.Manifest{Version: 1, SourceInstance: source, BotID: 123, DatePolicy: "preserve_source"}
	for _, role := range []string{"users", "messages", "passes", "files"} {
		records, ok := collections[role]
		if !ok {
			continue
		}
		var raw bytes.Buffer
		for _, record := range records {
			require.NoError(t, json.NewEncoder(&raw).Encode(record))
		}
		digest := sha256.Sum256(raw.Bytes())
		path := role + ".ndjson"
		require.NoError(t, os.WriteFile(filepath.Join(root, path), raw.Bytes(), 0o600))
		manifest.Collections = append(
			manifest.Collections,
			legacyimport.Collection{
				Role:                role,
				Name:                role,
				Path:                path,
				SHA256:              hex.EncodeToString(digest[:]),
				SingleBotCollection: role == "messages",
			},
		)
	}
	raw, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestPath := filepath.Join(root, "manifest.json")
	require.NoError(t, os.WriteFile(manifestPath, raw, 0o600))
	archivePath := filepath.Join(root, "archive.tar.gz")
	result, err := legacyimport.CreateBundle(manifestPath, archivePath)
	require.NoError(t, err)
	raw, err = os.ReadFile(archivePath)
	require.NoError(t, err)
	return raw, result.Archive.SHA256
}

func historyUser(id int64) map[string]any {
	return map[string]any{
		"_id":             "user",
		"bot_id":          123,
		"user_id":         id,
		"first_name":      "Historical name",
		"zitadel_subject": "untrusted-old-subject",
	}
}

func historyMessage(id, content string, owner int64) map[string]any {
	return map[string]any{
		"_id":     id,
		"user_id": owner,
		"role":    "user",
		"content": content,
		"date":    map[string]any{"$date": "2025-03-30T01:30:00+03:00"},
	}
}

func saveHistoryBundle(
	t *testing.T,
	store *db.Store,
	scope db.ProfileScope,
	raw []byte,
	digest string,
) db.HistoryApplyArguments {
	t.Helper()
	ctx := context.Background()
	r := db.LegacyArchiveRequest{OperationID: uuid.New(), Arguments: db.LegacyArchiveArguments{SHA256: digest}}
	prep, err := store.PrepareLegacyArchive(ctx, scope, r.OperationID, r.Arguments)
	require.NoError(t, err)
	r.ProposalID = prep.ID
	stored, err := store.StoreLegacyArchive(
		ctx,
		scope,
		r,
		func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil },
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	return db.HistoryApplyArguments{
		ArchiveID:         stored.ArchiveID,
		SHA256:            digest,
		ProjectionVersion: legacyimport.ProjectionVersion,
	}
}

func prepareHistory(
	t *testing.T,
	store *db.Store,
	scope db.ProfileScope,
	a db.HistoryApplyArguments,
) db.HistoryApplyRequest {
	t.Helper()
	r := db.HistoryApplyRequest{OperationID: uuid.New(), Arguments: a}
	prep, err := store.PrepareHistoryApply(context.Background(), scope, r.OperationID, a)
	require.NoError(t, err)
	r.ProposalID = prep.ID
	return r
}

func TestHistoryApplyIsAtomicIdempotentAndSeparateFromLiveBusiness(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	collections := map[string][]map[string]any{
		"users":    {historyUser(42)},
		"messages": {historyMessage("m1", "Original message", 42)},
		"passes":   {{"_id": "p1", "bot_id": 123, "user_id": 42, "paid": true, "pass_key": "old-event"}},
		"files":    {{"_id": "f1", "bot_id": 123, "user_id": 42, "file_id": "private-reference"}},
	}
	raw, digest := historyBundle(t, "legacy-test", collections)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, a)
	ctx := context.Background()
	noop := func(context.Context) error { return nil }
	receipt, err := h.store.ApplyHistory(ctx, scope, r, noop)
	require.NoError(t, err)
	require.EqualValues(t, 4, receipt.Records)
	require.EqualValues(t, 3, receipt.Projected)
	require.EqualValues(t, 4, receipt.Inserted)
	require.Zero(t, receipt.Reused)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_legacy_history_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", r.ProposalID)
	require.NoError(t, err)
	replay, err := h.store.ApplyHistory(ctx, scope, r, noop)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	status, err := h.store.HistoryApplyOperation(ctx, scope, r.OperationID)
	require.NoError(t, err)
	require.Equal(t, receipt, status)
	duplicate := prepareHistory(t, h.store, scope, a)
	replay, err = h.store.ApplyHistory(ctx, scope, duplicate, noop)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	wrong := r
	wrong.ProposalID = uuid.New()
	_, err = h.store.ApplyHistory(ctx, scope, wrong, noop)
	require.ErrorIs(t, err, db.ErrConflict)
	// A later snapshot adds one message and reuses all prior source identities.
	collections["messages"] = append(collections["messages"], historyMessage("m2", "Later message", 42))
	raw, digest = historyBundle(t, "legacy-test", collections)
	a = saveHistoryBundle(t, h.store, scope, raw, digest)
	later, err := h.store.ApplyHistory(ctx, scope, prepareHistory(t, h.store, scope, a), noop)
	require.NoError(t, err)
	require.EqualValues(t, 1, later.Inserted)
	require.EqualValues(t, 4, later.Reused)
	var records, memberships, users, tasks, registrations, payments int
	require.NoError(t, h.store.Pool().QueryRow(ctx, "SELECT\n(SELECT count(*) FROM shuttle_legacy_history_records),(SELECT cou" +
"nt(*) FROM shuttle_legacy_history_memberships),\n(SELECT count(*) FROM sh" +
"uttle_users),(SELECT count(*) FROM shuttle_tasks),\n(SELECT count(*) FROM" +
" shuttle_registrations),(SELECT count(*) FROM shuttle_payment_reviews)").Scan(&records, &memberships, &users, &tasks, &registrations, &payments))
	require.Equal(t, 5, records)
	require.Equal(t, 9, memberships)
	require.Equal(t, 1, users)
	require.Zero(t, tasks)
	require.Zero(t, registrations)
	require.Zero(t, payments)
	page, err := h.store.OwnerHistory(ctx, scope, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 4)
	require.False(t, page.CompleteTelegramHistory)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"paid_claim":true`)
	require.NotContains(t, string(encoded), "untrusted-old-subject")
	require.NotContains(t, string(encoded), "private-reference")
}

func TestHistoryConflictRollsBackNewPrefixAndPreservesOwner(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	ctx := context.Background()
	noop := func(context.Context) error { return nil }
	collections := map[string][]map[string]any{
		"users":    {historyUser(42)},
		"messages": {historyMessage("old", "Original", 42)},
	}
	raw, digest := historyBundle(t, "conflict-test", collections)
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	_, err := h.store.ApplyHistory(ctx, scope, prepareHistory(t, h.store, scope, a), noop)
	require.NoError(t, err)
	collections["messages"] = nil
	for i := range 205 {
		collections["messages"] = append(
			collections["messages"],
			historyMessage(uuid.NewString(), strings.Repeat("new", i+1), 42),
		)
	}
	collections["messages"] = append(collections["messages"], historyMessage("old", "Changed", 42))
	raw, digest = historyBundle(t, "conflict-test", collections)
	a = saveHistoryBundle(t, h.store, scope, raw, digest)
	r := prepareHistory(t, h.store, scope, a)
	_, err = h.store.ApplyHistory(ctx, scope, r, noop)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.HistoryApplyOperation(ctx, scope, r.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	page, err := h.store.OwnerHistory(ctx, scope, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	// Same source key with a different source owner must also fail as a whole.
	secondUser := historyUser(43)
	secondUser["_id"] = "second-user"
	collections["users"] = append(collections["users"], secondUser)
	collections["messages"] = []map[string]any{historyMessage("old", "Original", 43)}
	raw, digest = historyBundle(t, "conflict-test", collections)
	a = saveHistoryBundle(t, h.store, scope, raw, digest)
	_, err = h.store.ApplyHistory(ctx, scope, prepareHistory(t, h.store, scope, a), noop)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestHistoryRollbackAndRetryWithOnePoolConnection(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	parsed, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	single, err := db.Open(context.Background(), parsed.String())
	require.NoError(t, err)
	t.Cleanup(single.Close)
	collections := map[string][]map[string]any{"users": {historyUser(42)}}
	for range 210 {
		collections["messages"] = append(
			collections["messages"],
			historyMessage(uuid.NewString(), strings.Repeat("🙂long text", 800), 42),
		)
	}
	raw, digest := historyBundle(t, "single-connection", collections)
	a := saveHistoryBundle(t, single, scope, raw, digest)
	r := prepareHistory(t, single, scope, a)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = single.ApplyHistory(ctx, scope, r, func(context.Context) error { cancel(); return ctx.Err() })
	require.ErrorIs(t, err, context.Canceled)
	retry, retryCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer retryCancel()
	var records int
	require.NoError(
		t,
		single.Pool().QueryRow(retry, "SELECT count(*) FROM shuttle_legacy_history_records").Scan(&records),
	)
	require.Zero(t, records)
	receipt, err := single.ApplyHistory(retry, scope, r, func(context.Context) error { return nil })
	require.NoError(t, err)
	require.EqualValues(t, 211, receipt.Records)
}

func TestHistoryApplyRechecksAccountAndDeploymentBeforeCommit(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 42, true)
	scope := historyScope(t, h, id)
	ctx := context.Background()
	raw, digest := historyBundle(t, "revocation-test", map[string][]map[string]any{"users": {historyUser(42)}})
	a := saveHistoryBundle(t, h.store, scope, raw, digest)
	foreign := scope
	foreign.Organization = "other-org"
	_, err := h.store.PrepareHistoryApply(ctx, foreign, uuid.New(), a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	r := prepareHistory(t, h.store, scope, a)
	_, err = h.store.ApplyHistory(ctx, scope, r, func(ctx context.Context) error {
		_, revokeErr := h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", id)
		return revokeErr
	})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var records int
	require.NoError(
		t,
		h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_legacy_history_records").Scan(&records),
	)
	require.Zero(t, records)
}
