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

func telegramPrelinkClaim() db.TelegramPrelinkClaim {
	return db.TelegramPrelinkClaim{
		BotID:           1,
		TelegramUserID:  2,
		Organization:    "org",
		ProviderIssuer:  "https://provider.invalid/tg",
		ProviderID:      "idp",
		AssertionJTI:    "proof-one",
		ExternalSubject: "telegram:1:opaque-not-numeric",
		Profile:         json.RawMessage(`{"first_name":"Provider"}`),
		AssertionHash: sha256.Sum256(
			[]byte("signed-proof-one"),
		),
		ExpiresAt: time.Now().UTC().Truncate(time.Second).Add(time.Minute),
	}
}

func TestTelegramPrelinkSharesCanonicalUserAcrossEntryPoints(t *testing.T) {
	t.Parallel()
	for _, entry := range []string{"bot first", "miniapp first", "provider first"} {
		t.Run(entry, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			ctx := context.Background()
			claim := telegramPrelinkClaim()
			var userID uuid.UUID
			switch entry {
			case "bot first":
				userID = accepted(t, s, 100).UserID
			case "miniapp first":
				app, err := s.ClaimMiniAppSession(ctx, miniAppClaim())
				require.NoError(t, err)
				userID = app.User.ID
			case "provider first":
			}
			first, err := s.ClaimTelegramPrelink(ctx, claim)
			require.NoError(t, err)
			if userID != uuid.Nil {
				require.Equal(t, userID, first.User.ID)
			}
			require.Equal(t, "pending", first.State)
			require.Empty(t, first.Subject)
			require.Empty(t, first.User.Subject)
			if entry == "provider first" {
				app, appErr := s.ClaimMiniAppSession(ctx, miniAppClaim())
				require.NoError(t, appErr)
				require.Equal(t, first.User.ID, app.User.ID)
				require.Equal(t, first.User.ID, accepted(t, s, 101).UserID)
			}
			require.Equal(t, first.User.ID, accepted(t, s, 102).UserID)
		})
	}
}

func TestTelegramPrelinkDurableRetryCompletesWithoutNewConversation(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	claim := telegramPrelinkClaim()
	first, err := s.ClaimTelegramPrelink(ctx, claim)
	require.NoError(t, err)
	parsed, err := url.Parse(s.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	fresh, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	defer fresh.Close()
	retry, err := fresh.ClaimTelegramPrelink(ctx, claim)
	require.NoError(t, err)
	require.Equal(t, first, retry)
	// Claim released its only connection before the caller's IAM network phase.
	require.NoError(t, fresh.Pool().Ping(ctx))
	done, err := fresh.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
	require.NoError(t, err)
	require.Equal(t, "completed", done.State)
	require.Equal(t, "subject", done.Subject)
	require.Equal(t, "subject", done.User.Subject)
	again, err := s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
	require.NoError(t, err)
	require.Equal(t, done, again)
	replay, err := s.ClaimTelegramPrelink(ctx, claim)
	require.NoError(t, err)
	require.Equal(t, done, replay)
	for _, table := range []string{"shuttle_users", "shuttle_telegram_prelinks", "shuttle_conversations", "shuttle_tasks"} {
		var count int
		require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		if table == "shuttle_users" || table == "shuttle_telegram_prelinks" {
			require.Equal(t, 1, count)
		} else {
			require.Zero(t, count)
		}
	}
}

func TestTelegramPrelinkConcurrentRetriesConverge(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	claim := telegramPrelinkClaim()
	results := make(chan db.TelegramPrelinkAttempt, 8)
	errs := make(chan error, 8)
	var calls sync.WaitGroup
	for range cap(results) {
		calls.Go(func() { got, err := s.ClaimTelegramPrelink(ctx, claim); results <- got; errs <- err })
	}
	calls.Wait()
	close(results)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var first db.TelegramPrelinkAttempt
	for got := range results {
		if first.ID == uuid.Nil {
			first = got
		}
		require.Equal(t, first.ID, got.ID)
		require.Equal(t, first.User.ID, got.User.ID)
	}
	completed := make(chan error, 8)
	for range cap(completed) {
		calls.Go(func() { _, err := s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject"); completed <- err })
	}
	calls.Wait()
	close(completed)
	for err := range completed {
		require.NoError(t, err)
	}
	changed := claim
	changed.AssertionJTI = "fresh-proof"
	changed.AssertionHash = sha256.Sum256([]byte("fresh proof"))
	next, err := s.ClaimTelegramPrelink(ctx, changed)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, next.ID)
	require.Equal(t, first.User.ID, next.User.ID)
	_, err = s.CompleteTelegramPrelink(ctx, next.ID, changed, "subject")
	require.NoError(t, err)
}

func TestTelegramPrelinkRejectsChangedProofIntent(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"digest", "user", "bot", "organization", "provider", "external subject", "expiry"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			ctx := context.Background()
			original := telegramPrelinkClaim()
			first, err := s.ClaimTelegramPrelink(ctx, original)
			require.NoError(t, err)
			changed := original
			switch field {
			case "digest":
				changed.AssertionHash = sha256.Sum256([]byte("different signed bytes"))
			case "user":
				changed.TelegramUserID++
			case "bot":
				changed.BotID++
			case "organization":
				changed.Organization = "another-org"
			case "provider":
				changed.ProviderID = "another-provider"
			case "external subject":
				changed.ExternalSubject = "telegram:1:another-opaque"
			case "expiry":
				changed.ExpiresAt = changed.ExpiresAt.Add(-time.Second)
			}
			_, err = s.ClaimTelegramPrelink(ctx, changed)
			require.ErrorIs(t, err, db.ErrConflict)
			_, err = s.CompleteTelegramPrelink(ctx, first.ID, changed, "subject")
			require.Error(t, err)
			owner, err := s.User(ctx, first.User.ID)
			require.NoError(t, err)
			require.Empty(t, owner.Subject)
			var users int
			require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_users").Scan(&users))
			require.Equal(t, 1, users)
		})
	}
}

func TestTelegramPrelinkRechecksRevocationForPendingAndCompletedReceipts(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "completed"}[completed], func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			ctx := context.Background()
			claim := telegramPrelinkClaim()
			first, err := s.ClaimTelegramPrelink(ctx, claim)
			require.NoError(t, err)
			if completed {
				_, err = s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
				require.NoError(t, err)
			}
			require.NoError(t, s.RevokeUser(ctx, first.User.ID, true))
			_, err = s.ClaimTelegramPrelink(ctx, claim)
			require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
			_, err = s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
			require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
			owner, err := s.User(ctx, first.User.ID)
			require.NoError(t, err)
			require.True(t, owner.Revoked)
			if !completed {
				require.Empty(t, owner.Subject)
			}
		})
	}
}

func TestTelegramPrelinkCannotCompleteAfterProofExpires(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	claim := telegramPrelinkClaim()
	claim.ExpiresAt = time.Now().UTC().Truncate(time.Second).Add(3 * time.Second)
	first, err := s.ClaimTelegramPrelink(ctx, claim)
	require.NoError(t, err)
	// Expiry is checked against PostgreSQL wall time. The Docker database
	// clock can trail the Windows test process around a deadline.
	require.Eventually(t, func() bool {
		var now time.Time
		clockErr := s.Pool().QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
		return clockErr == nil && !now.Before(claim.ExpiresAt)
	}, 5*time.Second, 10*time.Millisecond)
	_, err = s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
	require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
	_, err = s.ClaimTelegramPrelink(ctx, claim)
	require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
	owner, err := s.User(ctx, first.User.ID)
	require.NoError(t, err)
	require.Empty(t, owner.Subject)
}

func TestTelegramPrelinkSubjectUniquenessIsAtomicAcrossUsers(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := context.Background()
	claim := telegramPrelinkClaim()
	first, err := s.ClaimTelegramPrelink(ctx, claim)
	require.NoError(t, err)
	_, err = s.CompleteTelegramPrelink(ctx, first.ID, claim, "subject")
	require.NoError(t, err)
	secondClaim := claim
	secondClaim.TelegramUserID++
	secondClaim.AssertionJTI = "second-user-proof"
	secondClaim.AssertionHash = sha256.Sum256([]byte("second-user"))
	secondClaim.ExternalSubject = "telegram:1:other-opaque"
	second, err := s.ClaimTelegramPrelink(ctx, secondClaim)
	require.NoError(t, err)
	_, err = s.CompleteTelegramPrelink(ctx, second.ID, secondClaim, "subject")
	require.ErrorIs(t, err, db.ErrConflict)
	owner, err := s.User(ctx, second.User.ID)
	require.NoError(t, err)
	require.Empty(t, owner.Subject)
	retry, err := s.ClaimTelegramPrelink(ctx, secondClaim)
	require.NoError(t, err)
	require.Equal(t, "pending", retry.State)
	_, err = s.BindSubject(ctx, second.User.ID, "subject", "org")
	require.Error(t, err, "Older writers must also be unable to create a duplicate IAM binding")
}
