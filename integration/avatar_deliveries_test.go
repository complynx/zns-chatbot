package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

func queuedAvatar(t *testing.T) (avatarDraftFixture, db.AvatarDraftReceipt, avatar.Rendered, avatarNativeKeys) {
	t.Helper()
	f, d, r, keys := avatarFinalizationFixture(t)
	receipt, err := f.h.store.CommitMiniAppAvatarDraft(
		t.Context(),
		f.scope,
		keys.bearer,
		keys.generation,
		db.AvatarDraftFinalize{OperationID: uuid.New(), DraftID: d.ID, ExpectedVersion: 1},
		db.AvatarDraftRender{Original: f.raw, Output: r},
	)
	require.NoError(t, err)
	return f, receipt, r, keys
}
func avatarDeployment(f avatarDraftFixture) db.EventScope {
	return db.EventScope{BotID: f.scope.BotID, Organization: f.scope.Organization}
}
func awaitDraftAvatarDelivery(t *testing.T, f avatarDraftFixture, id uuid.UUID, state string) db.AvatarDelivery {
	t.Helper()
	var n db.AvatarDelivery
	require.Eventually(t, func() bool {
		var err error
		n, err = f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), id)
		return err == nil && n.State == state
	}, 10*time.Second, 20*time.Millisecond)
	return n
}
func avatarDocumentCounts(f avatarDraftFixture) (int, int) {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	return len(f.p.documentAttempts), len(f.p.documentSent)
}
func enqueueAvatarDuplicate(t *testing.T, f avatarDraftFixture, id uuid.UUID, version int64) int64 {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"delivery_id": id, "version": version})
	require.NoError(t, err)
	var job int64
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `INSERT INTO river_job(kind,args,queue,state,max_attempts) VALUES('zinusya_avatar_delivery',$1,'default','available',25) RETURNING id`, raw).
			Scan(&job),
	)
	return job
}
func awaitAvatarDuplicate(t *testing.T, f avatarDraftFixture, job int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		var state string
		err := f.h.store.Pool().QueryRow(t.Context(), `SELECT state::text FROM river_job WHERE id=$1`, job).Scan(&state)
		return err == nil && state == "completed"
	}, 3*time.Second, 20*time.Millisecond)
}
func TestAvatarDeliverySendsNativeArtifactWithLoomDownAndExpiredSession(t *testing.T) {
	t.Parallel()
	f, receipt, r, keys := queuedAvatar(t)
	ctx := t.Context()
	require.NoError(t, f.h.store.LogoutMiniAppSession(ctx, f.scope.BotID, keys.bearer, keys.generation))
	f.p.mu.Lock()
	f.p.fileGone = true
	f.p.mu.Unlock()
	f.h.start(t)
	n := awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivered")
	// Persisted metadata distinguishes an acknowledged message from a queued artifact.
	var values struct {
		MessageID int64 `json:"message_id"`
		ChatID    int64 `json:"chat_id"`
	}
	require.NoError(t, json.Unmarshal(n.Receipt, &values))
	require.Positive(t, values.MessageID)
	require.Equal(t, int64(456), values.ChatID)
	f.p.mu.Lock()
	require.Len(t, f.p.documentSent, 1)
	document := f.p.documentSent[0]
	require.Empty(t, f.p.createKeys)
	require.Zero(t, f.p.fileGets)
	f.p.mu.Unlock()
	require.Equal(t, r.JPEG, document.Content)
	require.Equal(t, "zinusya-avatar.jpg", document.Filename)
	digest := sha256.Sum256(document.Content)
	require.Equal(t, receipt.SHA256, hex.EncodeToString(digest[:]))
	f.h.stop(t)
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivered")
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
}
func TestAvatarDeliveryUnknownReceiptIsNotRepeatedAfterRestart(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"lost", "wrong_chat"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, receipt, _, _ := queuedAvatar(t)
			f.p.mu.Lock()
			f.p.loseDocumentSend = kind == "lost"
			f.p.documentWrongReceipt = kind == "wrong_chat"
			f.p.mu.Unlock()
			f.h.start(t)
			awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
			f.h.stop(t)
			f.h.start(t)
			n := awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
			job := enqueueAvatarDuplicate(t, f, n.ID, n.Version)
			awaitAvatarDuplicate(t, f, job)
			attempts, sent := avatarDocumentCounts(f)
			require.Equal(t, 1, attempts)
			require.Equal(t, 1, sent)
			jobs, err := f.h.store.ReadyAvatarDeliveries(t.Context(), avatarDeployment(f), uuid.Nil)
			require.NoError(t, err)
			require.Empty(t, jobs)
		})
	}
}
func TestAvatarDeliveryDuplicateJobCannotChangeLiveAttempt(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	f.p.mu.Lock()
	f.p.documentEntered = entered
	f.p.documentRelease = release
	f.p.mu.Unlock()
	f.h.start(t)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Document send not reached")
	}
	n := awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "send_inflight")
	duplicate := enqueueAvatarDuplicate(t, f, n.ID, n.Version-1)
	require.Eventually(t, func() bool {
		var postponed bool
		err := f.h.store.Pool().
			QueryRow(t.Context(), `SELECT attempted_at IS NOT NULL AND state IN ('available','scheduled') FROM river_job WHERE id=$1`, duplicate).
			Scan(&postponed)
		return err == nil && postponed
	}, 2*time.Second, 10*time.Millisecond)
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
	unblock()
	awaitDraftAvatarDelivery(t, f, n.ID, "delivered")
	awaitAvatarDuplicate(t, f, duplicate)
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
}
func TestAvatarDeliveryRespectsTelegramRetryAfter(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	f.p.mu.Lock()
	f.p.documentRejectOnce = http.StatusTooManyRequests
	f.p.mu.Unlock()
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivered")
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	require.Len(t, f.p.documentAttempts, 2)
	require.Len(t, f.p.documentSent, 1)
	require.GreaterOrEqual(t, f.p.documentAttempts[1].Sub(f.p.documentAttempts[0]), 2*time.Second)
}
func TestAvatarDeliveryBlocksRevokedChangedOrIAMDeniedRecipient(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"revoked", "changed_recipient", "iam_denied"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, receipt, _, _ := queuedAvatar(t)
			ctx := t.Context()
			switch kind {
			case "revoked":
				_, err := f.h.store.Pool().
					Exec(ctx, `UPDATE shuttle_users SET revoked=true WHERE id=$1`, f.scope.UserID)
				require.NoError(t, err)
			case "changed_recipient":
				_, err := f.h.store.Pool().
					Exec(ctx, `UPDATE shuttle_users SET tg_user_id=457 WHERE id=$1`, f.scope.UserID)
				require.NoError(t, err)
			case "iam_denied":
				f.p.mu.Lock()
				f.p.userInactive = true
				f.p.mu.Unlock()
			}
			f.h.start(t)
			awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_blocked")
			attempts, sent := avatarDocumentCounts(f)
			require.Zero(t, attempts)
			require.Zero(t, sent)
		})
	}
}
func TestAvatarDeliveryWaitsForIAMAndResumesSameIntent(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	var attempts atomic.Int64
	f.p.mu.Lock()
	f.p.unavailableTokenSubjects = map[string]bool{f.scope.Subject: true}
	f.p.tokenHook = func(subject string) {
		if subject == f.scope.Subject {
			attempts.Add(1)
		}
	}
	f.p.mu.Unlock()
	f.h.start(t)
	require.Eventually(t, func() bool { return attempts.Load() > 0 }, 5*time.Second, 20*time.Millisecond)
	n, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), receipt.DeliveryID)
	require.NoError(t, err)
	require.Equal(t, "pending", n.State)
	requests, sent := avatarDocumentCounts(f)
	require.Zero(t, requests)
	require.Zero(t, sent)
	f.p.mu.Lock()
	delete(f.p.unavailableTokenSubjects, f.scope.Subject)
	f.p.mu.Unlock()
	awaitDraftAvatarDelivery(t, f, n.ID, "delivered")
	requests, sent = avatarDocumentCounts(f)
	require.Equal(t, 1, requests)
	require.Equal(t, 1, sent)
	avatarIntentCount(t, f, 1)
}
func TestAvatarDeliveryStartupRecoveryAndAttemptFencing(t *testing.T) {
	t.Parallel()
	f, receipt, r, _ := queuedAvatar(t)
	ctx := t.Context()
	reserved, content, err := f.h.store.ReserveAvatarDelivery(ctx, f.scope, receipt.DeliveryID, 1, uuid.New())
	require.NoError(t, err)
	require.Equal(t, r.JPEG, content)
	wrong := reserved
	wrong.AttemptID = uuid.New()
	require.ErrorIs(t, f.h.store.FinishAvatarDelivery(ctx, wrong, "delivered", nil, 0), db.ErrConflict)
	f.h.start(t)
	n := awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
	require.Greater(t, n.Version, reserved.Version)
	require.ErrorIs(t, f.h.store.FinishAvatarDelivery(ctx, reserved, "delivered", nil, 0), db.ErrConflict)
	attempts, sent := avatarDocumentCounts(f)
	require.Zero(t, attempts)
	require.Zero(t, sent)
}
func TestAvatarDeliverySafeRetryCannotBeFinishedByStaleAttempt(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	ctx := t.Context()
	reserved, _, err := f.h.store.ReserveAvatarDelivery(ctx, f.scope, receipt.DeliveryID, 1, uuid.New())
	require.NoError(t, err)
	require.NoError(t, f.h.store.FinishAvatarDelivery(ctx, reserved, "retryable", nil, time.Hour))
	n, err := f.h.store.AvatarDelivery(ctx, avatarDeployment(f), receipt.DeliveryID)
	require.NoError(t, err)
	_, _, err = f.h.store.ReserveAvatarDelivery(ctx, f.scope, n.ID, n.Version, uuid.New())
	require.ErrorIs(t, err, db.ErrConflict)
	jobs, err := f.h.store.ReadyAvatarDeliveries(ctx, avatarDeployment(f), uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, jobs)
	_, err = f.h.store.Pool().
		Exec(ctx, `UPDATE shuttle_avatar_deliveries SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE id=$1`, n.ID)
	require.NoError(t, err)
	next, _, err := f.h.store.ReserveAvatarDelivery(ctx, f.scope, n.ID, n.Version, uuid.New())
	require.NoError(t, err)
	require.NotEqual(t, reserved.AttemptID, next.AttemptID)
	require.ErrorIs(t, f.h.store.FinishAvatarDelivery(ctx, reserved, "delivered", nil, 0), db.ErrConflict)
	require.NoError(
		t,
		f.h.store.FinishAvatarDelivery(ctx, next, "delivered", json.RawMessage(`{"message_id":1,"chat_id":456}`), 0),
	)
}

func TestAvatarDeliveryFailedOutcomePersistenceDoesNotStrandIntent(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	_, err := f.h.store.Pool().Exec(t.Context(), `
 CREATE FUNCTION reject_avatar_retry_finish() RETURNS trigger LANGUAGE plpgsql AS $body$
 BEGIN
  IF OLD.state='send_inflight' AND NEW.state='retryable' THEN
   RAISE EXCEPTION 'fixture outcome persistence unavailable';
  END IF;
  RETURN NEW;
 END $body$;
 CREATE TRIGGER fixture_avatar_finish BEFORE UPDATE ON shuttle_avatar_deliveries
 FOR EACH ROW EXECUTE FUNCTION reject_avatar_retry_finish();`)
	require.NoError(t, err)
	f.p.mu.Lock()
	f.p.documentRejectOnce = http.StatusTooManyRequests
	f.p.mu.Unlock()
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Zero(t, sent)
}

func TestAvatarDeliveryGracefulStopPersistsUncertaintyWithoutDuplicate(t *testing.T) {
	t.Parallel()
	f, receipt, _, _ := queuedAvatar(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	f.p.mu.Lock()
	f.p.documentEntered = entered
	f.p.documentRelease = release
	f.p.mu.Unlock()
	f.h.start(t)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Document send not reached")
	}
	f.h.stop(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
	unblock()
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_unknown")
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
}

func TestAvatarDeliveryRechecksRecipientAfterIAMWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"revoked", "changed_recipient"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, receipt, _, _ := queuedAvatar(t)
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			f.p.mu.Lock()
			f.p.tokenHook = func(subject string) {
				if subject == f.scope.Subject {
					close(entered)
					<-release
				}
			}
			f.p.mu.Unlock()
			f.h.start(t)
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("IAM token request not reached")
			}
			query := `UPDATE shuttle_users SET revoked=true WHERE id=$1`
			if kind == "changed_recipient" {
				query = `UPDATE shuttle_users SET tg_user_id=457 WHERE id=$1`
			}
			_, err := f.h.store.Pool().Exec(t.Context(), query, f.scope.UserID)
			require.NoError(t, err)
			unblock()
			awaitDraftAvatarDelivery(t, f, receipt.DeliveryID, "delivery_blocked")
			attempts, sent := avatarDocumentCounts(f)
			require.Zero(t, attempts)
			require.Zero(t, sent)
		})
	}
}
