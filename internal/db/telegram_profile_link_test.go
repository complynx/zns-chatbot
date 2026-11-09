package db_test

import (
	"context"
	"crypto/sha256"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func completedProfileLink(t *testing.T, s *db.Store) (
	db.ProfileScope, db.TelegramPrelinkClaim, db.TelegramPrelinkAttempt,
) {
	t.Helper()
	claim := telegramPrelinkClaim()
	attempt, err := s.ClaimTelegramPrelink(t.Context(), claim)
	require.NoError(t, err)
	// Existing-name provenance is recorded before binding, as in the real flow.
	require.NoError(t, s.RecordIAMNameOrigin(t.Context(), attempt.User.ID, claim.BotID, claim.Organization, false))
	done, err := s.CompleteTelegramPrelink(t.Context(), attempt.ID, claim, "existing-human")
	require.NoError(t, err)
	scope := db.ProfileScope{
		UserID: done.User.ID, BotID: claim.BotID, Organization: claim.Organization, Subject: done.Subject,
	}
	return scope, claim, done
}

func TestCompletedTelegramProfileLinkAcceptsHistoricalCompletionAndIdenticalDuplicates(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	scope, claim, first := completedProfileLink(t, s)
	_, err := s.Pool().Exec(t.Context(), `UPDATE shuttle_telegram_prelinks
SET created_at=clock_timestamp()-interval '3 hours',completed_at=clock_timestamp()-interval '2 hours',
expires_at=date_trunc('second',clock_timestamp())-interval '1 hour' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
	require.NoError(t, err)
	require.NotNil(t, evidence)
	require.Equal(t, scope.UserID, evidence.UserID)
	require.Equal(t, scope.BotID, evidence.BotID)
	require.Equal(t, claim.TelegramUserID, evidence.TGID)
	require.Equal(t, scope.Subject, evidence.Subject)
	require.Equal(t, scope.Organization, evidence.Organization)
	require.Equal(t, claim.ProviderIssuer, evidence.ProviderIssuer)
	require.Equal(t, claim.ProviderID, evidence.ProviderID)
	require.Equal(t, claim.ExternalSubject, evidence.ExternalSubject)
	require.True(t, evidence.CompletedAt.Before(evidence.ExpiresAt))
	require.True(t, evidence.ExpiresAt.Before(time.Now()))
	// Historical profile evidence does not relax the assertion replay deadline.
	expiredClaim := claim
	expiredClaim.ExpiresAt = evidence.ExpiresAt
	_, err = s.CompleteTelegramPrelink(t.Context(), first.ID, expiredClaim, scope.Subject)
	require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
	claim.AssertionJTI = "same-link-new-proof"
	claim.AssertionHash = sha256.Sum256([]byte(claim.AssertionJTI))
	second, err := s.ClaimTelegramPrelink(t.Context(), claim)
	require.NoError(t, err)
	_, err = s.CompleteTelegramPrelink(t.Context(), second.ID, claim, scope.Subject)
	require.NoError(t, err)
	evidence, err = s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
	require.NoError(t, err)
	require.NotNil(t, evidence)
	require.Equal(t, claim.ExternalSubject, evidence.ExternalSubject)
}

func TestCompletedTelegramProfileLinkOnlyUsesCurrentScopeAndProvider(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{
		"absent", "pending", "old-subject", "old-organization", "old-bot", "provider-rotation", "issuer-rotation",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			scope, claim, done := completedProfileLink(t, s)
			var err error
			switch kind {
			case "absent":
				_, err = s.Pool().Exec(t.Context(), "DELETE FROM shuttle_telegram_prelinks WHERE id=$1", done.ID)
			case "pending":
				_, err = s.Pool().Exec(t.Context(), `UPDATE shuttle_telegram_prelinks
SET state='pending',subject='',completed_at=NULL WHERE id=$1`, done.ID)
			case "old-subject":
				_, err = s.Pool().Exec(t.Context(),
					"UPDATE shuttle_telegram_prelinks SET subject='former-subject' WHERE id=$1", done.ID)
			case "old-organization":
				_, err = s.Pool().Exec(t.Context(),
					"UPDATE shuttle_telegram_prelinks SET organization='former-org' WHERE id=$1", done.ID)
			case "old-bot":
				_, err = s.Pool().Exec(t.Context(), "UPDATE shuttle_telegram_prelinks SET bot_id=99 WHERE id=$1", done.ID)
			case "provider-rotation":
				claim.ProviderID = "current-provider"
			case "issuer-rotation":
				claim.ProviderIssuer = "https://current-provider.invalid"
			}
			require.NoError(t, err)
			evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
			require.NoError(t, err)
			require.Nil(t, evidence)
		})
	}
}

func TestCompletedTelegramProfileLinkAllowsNewProviderBesideHistoricalEvidence(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	scope, claim, _ := completedProfileLink(t, s)
	claim.ProviderID = "rotated-provider"
	claim.ProviderIssuer = "https://rotated.invalid/tg"
	claim.AssertionJTI = "current-provider-proof"
	claim.AssertionHash = sha256.Sum256([]byte(claim.AssertionJTI))
	claim.ExternalSubject = "new-opaque"
	attempt, err := s.ClaimTelegramPrelink(t.Context(), claim)
	require.NoError(t, err)
	_, err = s.CompleteTelegramPrelink(t.Context(), attempt.ID, claim, scope.Subject)
	require.NoError(t, err)
	evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
	require.NoError(t, err)
	require.NotNil(t, evidence)
	require.Equal(t, "new-opaque", evidence.ExternalSubject)
}

func TestCompletedTelegramProfileLinkRejectsAmbiguousOrInvalidCurrentProof(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"ambiguous", "future-completion", "invalid-opaque"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			scope, claim, done := completedProfileLink(t, s)
			switch kind {
			case "ambiguous":
				claim.AssertionJTI = "conflicting-current-proof"
				claim.AssertionHash = sha256.Sum256([]byte(claim.AssertionJTI))
				claim.ExternalSubject = "conflicting-opaque"
				attempt, err := s.ClaimTelegramPrelink(t.Context(), claim)
				require.NoError(t, err)
				_, err = s.CompleteTelegramPrelink(t.Context(), attempt.ID, claim, scope.Subject)
				require.NoError(t, err)
			case "future-completion":
				_, err := s.Pool().Exec(t.Context(), `UPDATE shuttle_telegram_prelinks
SET expires_at=clock_timestamp()+interval '2 hours',completed_at=clock_timestamp()+interval '1 hour'
WHERE id=$1`, done.ID)
				require.NoError(t, err)
			case "invalid-opaque":
				_, err := s.Pool().Exec(t.Context(),
					"UPDATE shuttle_telegram_prelinks SET external_subject=' ' WHERE id=$1", done.ID)
				require.NoError(t, err)
			}
			evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
			require.ErrorIs(t, err, db.ErrConflict)
			require.Nil(t, evidence)
		})
	}
}

func TestCompletedTelegramProfileLinkRejectsStaleBindingAndRestrictedOwner(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"rebound", "revoked", "ban-audit"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			s := testStore(t)
			scope, claim, _ := completedProfileLink(t, s)
			switch kind {
			case "rebound":
				_, err := s.Pool().Exec(t.Context(),
					"UPDATE shuttle_users SET zitadel_subject='new-canonical' WHERE id=$1", scope.UserID)
				require.NoError(t, err)
			case "revoked":
				_, err := s.SetUserRevoked(t.Context(), scope.UserID, "synthetic-revoke")
				require.NoError(t, err)
			case "ban-audit":
				_, err := s.BanTelegramUser(t.Context(), scope.BotID, claim.TelegramUserID, "synthetic-ban", "test", "")
				require.NoError(t, err)
				// Even inconsistent revocation flags cannot hide an existing ban.
				_, err = s.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=false WHERE id=$1", scope.UserID)
				require.NoError(t, err)
			}
			evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
			require.ErrorIs(t, err, db.ErrTelegramPrelinkDenied)
			require.Nil(t, evidence)
			if kind == "rebound" {
				scope.Subject = "new-canonical"
				evidence, err = s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
				require.NoError(t, err)
				require.Nil(t, evidence)
			}
		})
	}
}

func TestCompletedTelegramProfileLinkNativeNameCallbackUsesOneConnection(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	scope, claim, _ := completedProfileLink(t, s)
	u, err := url.Parse(s.Pool().Config().ConnString())
	require.NoError(t, err)
	query := u.Query()
	query.Set("pool_max_conns", "1")
	u.RawQuery = query.Encode()
	limited, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(limited.Close)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	profile, err := limited.ImportIAMProfileName(ctx, scope, func(ctx context.Context) (string, string, error) {
		evidence, readErr := limited.CompletedTelegramProfileLink(ctx, scope, claim.ProviderIssuer, claim.ProviderID)
		if readErr != nil {
			return "", "", readErr
		}
		require.NotNil(t, evidence)
		return "Existing", "Human", nil
	})
	require.NoError(t, err)
	require.Equal(t, "Existing Human", profile.FullName)
	require.NoError(t, limited.Pool().Ping(ctx))
}

func TestCompletedTelegramProfileLinkPropagatesDatabaseErrors(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	scope, claim, _ := completedProfileLink(t, s)
	s.Close()
	evidence, err := s.CompletedTelegramProfileLink(t.Context(), scope, claim.ProviderIssuer, claim.ProviderID)
	require.Error(t, err)
	require.NotErrorIs(t, err, db.ErrTelegramPrelinkDenied)
	require.Nil(t, evidence)
}
