package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/renderer"
	"github.com/complynx/zns-chatbot/internal/db"
)

type avatarNativeKeys struct{ bearer, generation [sha256.Size]byte }

func avatarFinalizationFixture(t *testing.T) (avatarDraftFixture, db.AvatarDraft, avatar.Rendered, avatarNativeKeys) {
	t.Helper()
	f := avatarDraftSetup(t, nil)
	code, raw := avatarUpload(t, f, uuid.New(), f.raw)
	require.Equal(t, http.StatusOK, code, string(raw))
	d := avatarDraftMetadata(t, raw)
	f.h.stop(t)
	rendered, err := renderer.Render(t.Context(), f.raw, d.Transform)
	require.NoError(t, err)
	var bearer, generation []byte
	err = f.h.store.Pool().
		QueryRow(t.Context(), "SELECT bearer_hash,key_generation FROM shuttle_miniapp_sessions"+
			" WHERE user_id=$1 AND state='active'", f.scope.UserID).
		Scan(&bearer, &generation)
	require.NoError(t, err)
	require.Len(t, bearer, sha256.Size)
	require.Len(t, generation, sha256.Size)
	return f, d, rendered, avatarNativeKeys{[sha256.Size]byte(bearer), [sha256.Size]byte(generation)}
}
func avatarIntentCount(t *testing.T, f avatarDraftFixture, want int) {
	t.Helper()
	var artifacts, intents int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_avatars WHERE user_id=$1),"+
				"(SELECT count(*) FROM shuttle_avatar_deliveries WHERE user_id=$1)", f.scope.UserID).
			Scan(&artifacts, &intents),
	)
	require.Equal(t, want, artifacts)
	require.Equal(t, want, intents)
}
func TestAvatarFinalizationStoresImmutableSnapshotAndIntentWithoutLoom(t *testing.T) {
	t.Parallel()
	f, d, _, keys := avatarFinalizationFixture(t)
	ctx := t.Context()
	d, err := f.h.store.SaveAvatarDraft(
		ctx,
		f.scope,
		db.AvatarDraftSave{
			OperationID:     uuid.New(),
			DraftID:         d.ID,
			ExpectedVersion: 1,
			Transform:       avatar.Transform{ZoomPPM: 2 * avatar.FramePPM, AngleMilli: 90000},
		},
	)
	require.NoError(t, err)
	r, err := renderer.Render(ctx, f.raw, d.Transform)
	require.NoError(t, err)
	a := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: d.Version}
	prior, err := f.h.store.AvatarDraftFinalizePreflight(ctx, f.scope, a)
	require.NoError(t, err)
	require.Nil(t, prior)
	receipt, err := f.h.store.CommitMiniAppAvatarDraft(
		ctx,
		f.scope,
		keys.bearer,
		keys.generation,
		a,
		db.AvatarDraftRender{Original: f.raw, Output: r},
	)
	require.NoError(t, err)
	require.Equal(t, d.ID, receipt.DraftID)
	require.Equal(t, int64(2), receipt.DraftVersion)
	require.Equal(t, d.Transform, receipt.Transform)
	require.Equal(t, avatar.EditedProcessorVersion, receipt.ProcessorVersion)
	require.NoError(t, f.h.store.LogoutMiniAppSession(ctx, f.scope.BotID, keys.bearer, keys.generation))
	avatarIntentCount(t, f, 1)
	var state string
	var task, outbox int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx,
				"SELECT state FROM shuttle_avatar_deliveries WHERE id=$1 AND artifact_id=$2",
				receipt.DeliveryID, receipt.ArtifactID).
			Scan(&state),
	)
	require.Equal(t, "pending", state)
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT (SELECT count(*) FROM shuttle_tasks),(SELECT count(*) FROM shuttle_outbox)`).
			Scan(&task, &outbox),
	)
	require.Zero(t, task)
	require.Zero(t, outbox)
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
	require.NoError(t, err)
	replay, err := f.h.store.CommitAvatarDraft(
		ctx,
		f.scope,
		a,
		db.AvatarDraftRender{Original: nil, Output: avatar.Rendered{}},
	)
	require.NoError(t, err)
	require.Equal(t, receipt, replay)
	prior, err = f.h.store.AvatarDraftFinalizePreflight(ctx, f.scope, a)
	require.NoError(t, err)
	require.Equal(t, &receipt, prior)
	original, raw, err := f.h.store.GetFinalizedAvatar(ctx, f.scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, original)
	require.Equal(t, r.JPEG, raw)
	recovered, err := f.h.store.AvatarDraftFinalizationOperation(ctx, f.scope, a.OperationID)
	require.NoError(t, err)
	require.Equal(t, receipt, recovered)
	avatarIntentCount(t, f, 1)
}
func TestAvatarFinalizationRejectsStaleForeignSessionAndWrongProvenance(t *testing.T) {
	t.Parallel()
	f, d, r, keys := avatarFinalizationFixture(t)
	ctx := t.Context()
	a := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1}
	wrong := r
	wrong.SourceSHA256 = strings.Repeat("a", 64)
	_, err := f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: wrong})
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	wrong = r
	wrong.FrameSHA256 = strings.Repeat("b", 64)
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: wrong})
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	wrong = r
	wrong.ProcessorVersion = "unexpected"
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: wrong})
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	wrong = r
	edited := avatar.Transform{ZoomPPM: 2 * avatar.FramePPM}
	wrong.Transform = &edited
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: wrong})
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	wrong = r
	wrong.JPEG = []byte("not JPEG")
	sum := sha256.Sum256(wrong.JPEG)
	wrong.SHA256 = hex.EncodeToString(sum[:])
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: wrong})
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	a.ExpectedVersion = 2
	_, err = f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: r})
	require.ErrorIs(t, err, db.ErrConflict)
	a.ExpectedVersion = 1
	otherID, _ := registrationUser(t, f.h, 457, false)
	other := ownerProfileScope(t, f.h, otherID)
	_, err = f.h.store.CommitAvatarDraft(ctx, other, a, db.AvatarDraftRender{Original: f.raw, Output: r})
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, f.h.store.LogoutMiniAppSession(ctx, f.scope.BotID, keys.bearer, keys.generation))
	_, err = f.h.store.CommitMiniAppAvatarDraft(
		ctx,
		f.scope,
		keys.bearer,
		keys.generation,
		a,
		db.AvatarDraftRender{Original: f.raw, Output: r},
	)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	avatarIntentCount(t, f, 0)
	receipt, err := f.h.store.CommitAvatarDraft(ctx, f.scope, a, db.AvatarDraftRender{Original: f.raw, Output: r})
	require.NoError(t, err)
	_, _, err = f.h.store.GetFinalizedAvatar(ctx, other, receipt.ArtifactID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.h.store.AvatarDraftFinalizationOperation(ctx, other, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	a.ExpectedVersion = 2
	_, err = f.h.store.AvatarDraftFinalizePreflight(ctx, f.scope, a)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.Pool().Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, f.scope.UserID)
	require.NoError(t, err)
	_, _, err = f.h.store.GetFinalizedAvatar(ctx, f.scope, receipt.ArtifactID)
	require.Error(t, err)
}
func TestAvatarFinalizationConcurrentCrossPathReplayAndSharedQuota(t *testing.T) {
	t.Parallel()
	f, d, r, keys := avatarFinalizationFixture(t)
	ctx := t.Context()
	a := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1}
	var results [2]db.AvatarDraftReceipt
	var failures [2]error
	var group sync.WaitGroup
	start := make(chan struct{})
	for i := range 2 {
		group.Go(func() {
			<-start
			if i == 0 {
				results[i], failures[i] = f.h.store.CommitAvatarDraft(
					ctx,
					f.scope,
					a,
					db.AvatarDraftRender{Original: f.raw, Output: r},
				)
			} else {
				results[i], failures[i] = f.h.store.CommitMiniAppAvatarDraft(
					ctx,
					f.scope,
					keys.bearer,
					keys.generation,
					a,
					db.AvatarDraftRender{Original: f.raw, Output: r},
				)
			}
		})
	}
	close(start)
	group.Wait()
	require.NoError(t, failures[0])
	require.NoError(t, failures[1])
	require.Equal(t, results[0], results[1])
	avatarIntentCount(t, f, 1)
	for range db.MaxAvatarsPerOwner - 1 {
		next := a
		next.OperationID = uuid.New()
		_, err := f.h.store.CommitAvatarDraft(ctx, f.scope, next, db.AvatarDraftRender{Original: f.raw, Output: r})
		require.NoError(t, err)
	}
	next := a
	next.OperationID = uuid.New()
	_, err := f.h.store.CommitAvatarDraft(ctx, f.scope, next, db.AvatarDraftRender{Original: f.raw, Output: r})
	require.ErrorIs(t, err, db.ErrAvatarQuota)
	avatarIntentCount(t, f, db.MaxAvatarsPerOwner)
	replay, err := f.h.store.CommitAvatarDraft(
		ctx,
		f.scope,
		a,
		db.AvatarDraftRender{Original: nil, Output: avatar.Rendered{}},
	)
	require.NoError(t, err)
	require.Equal(t, results[0], replay)
}
func TestAvatarFinalizationPreservesLegacyReceiptAndOriginIsolation(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	f.h.stop(t)
	scope := ownerProfileScope(t, f.h, f.ownerID)
	ctx := t.Context()
	legacy := db.AvatarRequest{OperationID: uuid.New(), RunID: f.runID, SourceFileID: f.source}
	old, err := f.h.store.CommitAvatar(ctx, scope, legacy, f.rendered)
	require.NoError(t, err)
	var before []byte
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT receipt::text FROM shuttle_avatars WHERE id=$1`, old.ArtifactID).
			Scan(&before),
	)
	d, err := f.h.store.CreateAvatarDraft(
		ctx,
		scope,
		db.AvatarDraftCreate{OperationID: uuid.New(), SourceFileID: f.source},
		f.p.fileBytes,
		f.rendered,
	)
	require.NoError(t, err)
	a := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1}
	result, err := f.h.store.CommitAvatarDraft(
		ctx,
		scope,
		a,
		db.AvatarDraftRender{Original: f.p.fileBytes, Output: f.rendered},
	)
	require.NoError(t, err)
	_, err = f.h.store.AvatarOperation(ctx, scope, a.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, _, err = f.h.store.GetAvatar(ctx, scope, result.ArtifactID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, _, err = f.h.store.GetFinalizedAvatar(ctx, scope, old.ArtifactID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	legacyOperation := legacy.OperationID
	legacy.OperationID = a.OperationID
	_, err = f.h.store.AvatarPreflight(ctx, scope, legacy)
	require.ErrorIs(t, err, db.ErrConflict)
	a.OperationID = legacyOperation
	_, err = f.h.store.AvatarDraftFinalizePreflight(ctx, scope, a)
	require.ErrorIs(t, err, db.ErrConflict)
	current, raw, err := f.h.store.GetAvatar(ctx, scope, old.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, old, current)
	require.Equal(t, f.rendered.JPEG, raw)
	var after []byte
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, `SELECT receipt::text FROM shuttle_avatars WHERE id=$1`, old.ArtifactID).
			Scan(&after),
	)
	require.Equal(t, before, after)
	for range db.MaxAvatarsPerOwner - 2 {
		next := db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1}
		_, err = f.h.store.CommitAvatarDraft(
			ctx,
			scope,
			next,
			db.AvatarDraftRender{Original: f.p.fileBytes, Output: f.rendered},
		)
		require.NoError(t, err)
	}
	legacy.OperationID = uuid.New()
	_, err = f.h.store.CommitAvatar(ctx, scope, legacy, f.rendered)
	require.ErrorIs(t, err, db.ErrAvatarQuota)
	a.OperationID = uuid.New()
	_, err = f.h.store.CommitAvatarDraft(
		ctx,
		scope,
		a,
		db.AvatarDraftRender{Original: f.p.fileBytes, Output: f.rendered},
	)
	require.ErrorIs(t, err, db.ErrAvatarQuota)
}
func TestAvatarFinalizationProtectsHistoryAndPartialOrigins(t *testing.T) {
	t.Parallel()
	f, d, r, _ := avatarFinalizationFixture(t)
	f.h.stop(t)
	ctx := t.Context()
	receipt, err := f.h.store.CommitAvatarDraft(
		ctx,
		f.scope,
		db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1},
		db.AvatarDraftRender{Original: f.raw, Output: r},
	)
	require.NoError(t, err)
	for _, change := range []string{
		"draft_version=NULL",
		"draft_id=NULL,draft_version=NULL",
		"task_id='11111111-1111-1111-1111-111111111111'::uuid",
	} {
		_, err = f.h.store.Pool().Exec(ctx, "UPDATE shuttle_avatars SET "+change+" WHERE id=$1", receipt.ArtifactID)
		var rejected *pgconn.PgError
		require.ErrorAs(t, err, &rejected)
		require.Equal(t, "23514", rejected.Code, change)
	}
	current, raw, err := f.h.store.GetFinalizedAvatar(ctx, f.scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, current)
	require.Equal(t, r.JPEG, raw)
	avatarIntentCount(t, f, 1)
}
