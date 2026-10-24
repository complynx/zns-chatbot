package db_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func miniAppClaim() db.MiniAppClaim {
	return db.MiniAppClaim{
		BotID:          1,
		TelegramUserID: 2,
		Organization:   "org",
		Profile:        json.RawMessage(`{"first_name":"Initial"}`),
		ProofHash: sha256.Sum256(
			[]byte("proof"),
		),
		NonceHash:  sha256.Sum256([]byte("nonce")),
		Generation: sha256.Sum256([]byte("generation")),
		AuthDate: time.Now().
			UTC().
			Truncate(time.Second),
		MaxAge:     5 * time.Minute,
		PendingTTL: 2 * time.Minute,
		SessionTTL: 15 * time.Minute,
	}
}

func miniAppActivation(claim db.MiniAppClaim, attempt db.MiniAppAttempt) db.MiniAppActivation {
	return db.MiniAppActivation{
		ID:           attempt.ID,
		BotID:        claim.BotID,
		NonceHash:    claim.NonceHash,
		Generation:   claim.Generation,
		BearerHash:   sha256.Sum256([]byte("bearer")),
		Subject:      "miniapp-user",
		Organization: claim.Organization,
	}
}

func TestMiniAppPendingRetryPreservesIdentityAndCreatesNoConversation(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	ctx := context.Background()
	claim := miniAppClaim()
	first, err := store.ClaimMiniAppSession(ctx, claim)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, first.User.ID)
	require.Empty(t, first.User.Subject, "Proposed local UUID is durable before any IAM call")
	var group sync.WaitGroup
	for range 6 {
		group.Go(func() {
			got, claimErr := store.ClaimMiniAppSession(ctx, claim)
			if claimErr != nil {
				t.Error(claimErr)
				return
			}
			if got.ID != first.ID || got.User.ID != first.User.ID || !got.ExpiresAt.Equal(first.ExpiresAt) {
				t.Error("Concurrent retry replaced identity, session or expiry")
			}
		})
	}
	group.Wait()
	for _, table := range []string{"shuttle_users", "shuttle_miniapp_sessions", "shuttle_conversations", "shuttle_tasks"} {
		var count int
		require.NoError(t, store.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		if table == "shuttle_users" || table == "shuttle_miniapp_sessions" {
			require.Equal(t, 1, count)
		} else {
			require.Zero(t, count)
		}
	}
	_, err = store.Pool().
		Exec(ctx, "UPDATE shuttle_users SET profile='{\"first_name\":\"New Telegram snapshot\"}'::jsonb "+
			"WHERE id=$1", first.User.ID)
	require.NoError(t, err)
	claim.SessionTTL = time.Hour
	retry, err := store.ClaimMiniAppSession(ctx, claim)
	require.NoError(t, err)
	require.JSONEq(t, `{"first_name":"New Telegram snapshot"}`, string(retry.User.Profile))
	require.Equal(t, first.ExpiresAt, retry.ExpiresAt, "Retry must not extend the original session")
	wrong := claim
	wrong.NonceHash = sha256.Sum256([]byte("other nonce"))
	_, err = store.ClaimMiniAppSession(ctx, wrong)
	require.ErrorIs(t, err, db.ErrConflict)
	activation := miniAppActivation(claim, first)
	active, err := store.ActivateMiniAppSession(ctx, activation)
	require.NoError(t, err)
	require.Equal(t, "active", active.State)
	require.Equal(t, first.ExpiresAt, active.ExpiresAt)
	require.Equal(t, activation.Subject, active.User.Subject)
	activeRetry, err := store.ActivateMiniAppSession(ctx, activation)
	require.NoError(t, err)
	require.Equal(t, active, activeRetry)
	parsed, err := url.Parse(store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	reopened, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	read, err := reopened.MiniAppSession(ctx, claim.BotID, activation.BearerHash, claim.Generation)
	require.NoError(t, err)
	require.Equal(t, active, read, "A fresh one-connection store recovers the durable session")
	require.NoError(t, reopened.LogoutMiniAppSession(ctx, claim.BotID, activation.BearerHash, claim.Generation))
	_, err = reopened.MiniAppSession(ctx, claim.BotID, activation.BearerHash, claim.Generation)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = store.ClaimMiniAppSession(ctx, claim)
	require.ErrorIs(t, err, db.ErrMiniAppDenied, "Logout consumes the proof; retry cannot resurrect it")
	_, err = store.ActivateMiniAppSession(ctx, activation)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
}

func TestMiniAppActivationAndReadsRecheckCurrentAuthority(t *testing.T) {
	t.Parallel()
	cases := []string{"pending expired", "account revoked", "binding changed", "generation changed", "active expired"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := testStore(t)
			ctx := context.Background()
			claim := miniAppClaim()
			attempt, err := store.ClaimMiniAppSession(ctx, claim)
			require.NoError(t, err)
			activation := miniAppActivation(claim, attempt)
			if name == "pending expired" {
				_, err = store.Pool().
					Exec(ctx, "UPDATE shuttle_miniapp_sessions SET pending_until=clock_timestamp()-interval '1 "+
						"second' WHERE id=$1", attempt.ID)
				require.NoError(t, err)
				_, err = store.ActivateMiniAppSession(ctx, activation)
				require.ErrorIs(t, err, db.ErrMiniAppDenied)
				owner, readErr := store.User(ctx, attempt.User.ID)
				require.NoError(t, readErr)
				require.Empty(t, owner.Subject)
				return
			}
			_, err = store.ActivateMiniAppSession(ctx, activation)
			require.NoError(t, err)
			switch name {
			case "account revoked":
				err = store.RevokeUser(ctx, attempt.User.ID, true)
			case "binding changed":
				_, err = store.Pool().
					Exec(ctx, "UPDATE shuttle_users SET zitadel_subject='changed' WHERE id=$1", attempt.User.ID)
			case "generation changed":
				claim.Generation = sha256.Sum256([]byte("rotated"))
				activation.Generation = claim.Generation
			case "active expired":
				_, err = store.Pool().
					Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' "+
						"WHERE id=$1", attempt.ID)
			}
			require.NoError(t, err)
			_, err = store.MiniAppSession(ctx, claim.BotID, activation.BearerHash, claim.Generation)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = store.ClaimMiniAppSession(ctx, claim)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = store.ActivateMiniAppSession(ctx, activation)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
		})
	}
}

func TestMiniAppPruningRetainsConsumedProofForMaximumFreshness(t *testing.T) {
	t.Parallel()
	store := testStore(t)
	ctx := context.Background()
	claim := miniAppClaim()
	first, err := store.ClaimMiniAppSession(ctx, claim)
	require.NoError(t, err)
	_, err = store.Pool().
		Exec(ctx, "UPDATE shuttle_miniapp_sessions SET pending_until=clock_timestamp()-interval '1 "+
			"second',expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", first.ID)
	require.NoError(t, err)
	secondClaim := claim
	secondClaim.ProofHash = sha256.Sum256([]byte("second proof"))
	_, err = store.ClaimMiniAppSession(ctx, secondClaim)
	require.NoError(t, err)
	var count int
	require.NoError(
		t,
		store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_miniapp_sessions WHERE id=$1", first.ID).Scan(&count),
	)
	require.Equal(t, 1, count, "Changing lifetime policy cannot resurrect an already consumed proof")
	_, err = store.Pool().
		Exec(ctx, "UPDATE shuttle_miniapp_sessions SET auth_date=clock_timestamp()-interval '25 hours' WHERE id=$1", first.ID)
	require.NoError(t, err)
	_, err = store.ClaimMiniAppSession(ctx, secondClaim)
	require.NoError(t, err)
	require.NoError(
		t,
		store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_miniapp_sessions WHERE id=$1", first.ID).Scan(&count),
	)
	require.Zero(t, count, "Expired sessions past the hard freshness ceiling can be removed")
}
