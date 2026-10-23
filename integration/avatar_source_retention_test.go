package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
)

func avatarSourceSettings(t *testing.T, f avatarDraftFixture, limit int64, seconds int) {
	t.Helper()
	settings, err := f.h.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.AvatarBrowserStorageLimitBytes = limit
	settings.Document.AvatarBrowserRetentionSeconds = seconds
	_, err = f.h.store.CompareAndSwapSettings(t.Context(), settings.Version, settings.Document, "fixture-admin")
	require.NoError(t, err)
}

func TestAvatarBrowserExpiryPreservesCompletedJPEGAndImmutableReceipts(t *testing.T) {
	t.Parallel()
	f, d, r, keys := avatarFinalizationFixture(t)
	ctx := t.Context()
	request := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: d.Version}
	receipt, err := f.h.store.CommitMiniAppAvatarDraft(
		ctx,
		f.scope,
		keys.bearer,
		keys.generation,
		request,
		db.AvatarDraftRender{Original: f.raw, Output: r},
	)
	require.NoError(t, err)
	var before string
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT result::text FROM shuttle_avatar_draft_operations WHERE draft_id=$1`, d.ID).
			Scan(&before),
	)
	_, err = f.h.store.Pool().
		Exec(ctx, `UPDATE shuttle_avatar_drafts SET created_at=clock_timestamp()-interval '91 days' WHERE id=$1`, d.ID)
	require.NoError(t, err)
	metadata, err := f.h.store.GetAvatarDraft(ctx, f.scope, d.ID)
	require.NoError(t, err)
	require.True(t, metadata.SourceExpired)
	_, _, err = f.h.store.AvatarDraftOriginal(ctx, f.scope, d.ID)
	require.ErrorIs(t, err, db.ErrAvatarSourceExpired)
	_, err = f.h.store.SaveAvatarDraft(
		ctx,
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: d.Version,
			Transform:       avatar.IdentityTransform(),
		},
	)
	require.ErrorIs(t, err, db.ErrAvatarSourceExpired)
	late := request
	late.OperationID = uuid.New()
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, late, db.AvatarDraftRender{Original: f.raw, Output: r})
	require.ErrorIs(t, err, db.ErrAvatarSourceExpired, "rendered before expiry must not commit after expiry")
	scope := db.EventScope{BotID: f.scope.BotID, Organization: f.scope.Organization}
	_, err = f.h.store.ExpireAvatarSources(ctx, scope, uuid.Nil)
	require.NoError(t, err)
	var retained int
	var after string
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT coalesce(octet_length(original),0) FROM shuttle_avatar_drafts WHERE id=$1`, d.ID).
			Scan(&retained),
	)
	require.Zero(t, retained)
	current, err := f.h.store.GetAvatarDraft(ctx, f.scope, d.ID)
	require.NoError(t, err)
	require.Equal(t, metadata, current, "cleanup must not edit the draft or its version")
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT result::text FROM shuttle_avatar_draft_operations WHERE draft_id=$1`, d.ID).
			Scan(&after),
	)
	require.Equal(t, before, after)
	replay, err := f.h.store.CommitAvatarDraft(ctx, f.scope, request, db.AvatarDraftRender{})
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	got, jpeg, err := f.h.store.GetFinalizedAvatar(ctx, f.scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, got)
	require.Equal(t, r.JPEG, jpeg)
	avatarIntentCount(t, f, 1)
	// A later policy increase cannot recover purged bytes; a new upload has room.
	avatarSourceSettings(t, f, 0, 60)
	current, err = f.h.store.GetAvatarDraft(ctx, f.scope, d.ID)
	require.NoError(t, err)
	require.True(t, current.SourceExpired)
	list, err := f.h.store.ListAvatarDrafts(ctx, f.scope)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestAvatarBrowserQuotaSerializesDifferentOwnersAndAllowsReplay(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	f.h.stop(t)
	other, _ := registrationUser(t, f.h, 457, false)
	scopes := []db.ProfileScope{f.scope, ownerProfileScope(t, f.h, other)}
	avatarSourceSettings(t, f, int64(len(f.raw)), 0)
	digest := sha256.Sum256(f.raw)
	rendered, err := renderer.Render(t.Context(), f.raw, avatar.IdentityTransform())
	require.NoError(t, err)
	requests := []db.AvatarDraftCreate{
		{OperationID: uuid.New(), UploadSHA256: hex.EncodeToString(digest[:])},
		{OperationID: uuid.New(), UploadSHA256: hex.EncodeToString(digest[:])},
	}
	results := make([]db.AvatarDraft, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range scopes {
		wg.Go(func() {
			results[i], errs[i] = f.h.store.CreateAvatarDraft(t.Context(), scopes[i], requests[i], f.raw, rendered)
		})
	}
	wg.Wait()
	success := 0
	for i, err := range errs {
		if err != nil {
			require.ErrorIs(t, err, db.ErrAvatarDraftQuota)
			continue
		}
		success++
		replay, replayErr := f.h.store.CreateAvatarDraft(t.Context(), scopes[i], requests[i], f.raw, rendered)
		require.NoError(t, replayErr)
		require.Equal(t, results[i], replay)
	}
	require.Equal(t, 1, success)
	var total int64
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `SELECT coalesce(sum(source_size_bytes),0)::bigint FROM shuttle_avatar_drafts WHERE original IS NOT NULL`).
			Scan(&total),
	)
	require.Equal(t, int64(len(f.raw)), total)
	avatarSourceSettings(t, f, 1, 0)
	for i, err := range errs {
		if err == nil {
			_, raw, readErr := f.h.store.AvatarDraftOriginal(t.Context(), scopes[i], results[i].ID)
			require.NoError(t, readErr)
			require.Equal(t, f.raw, raw)
		}
	}
}

func TestAvatarBrowserExpiredOriginalReturnsGoneAndCanBeReplaced(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	op := uuid.New()
	code, body := avatarUpload(t, f, op, f.raw)
	require.Equal(t, http.StatusOK, code, string(body))
	d := avatarDraftMetadata(t, body)
	require.NotNil(t, d.SourceExpiresAt)
	_, err := f.h.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_avatar_drafts SET created_at=clock_timestamp()-interval '91 days' WHERE id=$1`, d.ID)
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.h,
		http.MethodGet,
		"/app/avatars/drafts/"+d.ID.String()+"/original",
		"",
		f.session.Token,
		"",
	)
	require.Equal(t, http.StatusGone, code, string(raw))
	require.Eventually(t, func() bool {
		var retained bool
		readErr := f.h.store.Pool().
			QueryRow(t.Context(), `SELECT original IS NOT NULL FROM shuttle_avatar_drafts WHERE id=$1`, d.ID).
			Scan(&retained)
		return readErr == nil && !retained
	}, 5*time.Second, 20*time.Millisecond, "maintenance must expire an idle owner's original without an upload")
	code, raw = avatarUpload(t, f, op, f.raw)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(body), string(raw))
	code, raw = avatarUpload(t, f, uuid.New(), f.raw)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotEqual(t, d.ID, avatarDraftMetadata(t, raw).ID)
}

func TestAvatarBrowserRetentionPreservesDraftAfterSourceExpiry(t *testing.T) {
	t.Parallel()
	f := avatarDraftSetup(t, nil)
	code, body := avatarUpload(t, f, uuid.New(), f.raw)
	require.Equal(t, http.StatusOK, code, string(body))
	d := avatarDraftMetadata(t, body)
	f.h.stop(t)
	ctx := t.Context()
	current, original, err := f.h.store.AvatarDraftOriginal(ctx, f.scope, d.ID)
	require.NoError(t, err)
	require.Equal(t, d, current)
	require.Equal(t, f.raw, original)
	_, err = f.h.store.Pool().
		Exec(ctx, `UPDATE shuttle_avatar_drafts SET created_at=clock_timestamp()-interval '91 days' WHERE id=$1`, d.ID)
	require.NoError(t, err)
	_, err = f.h.store.ExpireAvatarSources(
		ctx,
		db.EventScope{BotID: f.scope.BotID, Organization: f.scope.Organization},
		uuid.Nil,
	)
	require.NoError(t, err)
	current, err = f.h.store.GetAvatarDraft(ctx, f.scope, d.ID)
	require.NoError(t, err)
	require.True(t, current.SourceExpired)
}
