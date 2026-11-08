package db_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/symbols"
)

func TestBanTelegramUserBeforeOnboardingAndReplay(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	first, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test access restriction")
	require.NoError(t, err)
	require.True(t, first.Revoked)
	require.False(t, first.PreviouslyRevoked)
	require.NotEqual(t, uuid.Nil, first.ID)
	owner, err := s.GetUser(ctx, first.UserID)
	require.NoError(t, err)
	require.Empty(t, owner.Subject)
	require.Empty(t, owner.Organization)
	require.JSONEq(t, `{}`, string(owner.Profile))
	var group sync.WaitGroup
	for range 6 {
		group.Go(func() {
			got, replayErr := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test access restriction")
			if replayErr != nil {
				t.Error(replayErr)
				return
			}
			if got != first {
				t.Error("ban replay changed the original receipt")
			}
		})
	}
	group.Wait()
	reopened, err := db.Open(ctx, s.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	replay, err := reopened.BanTelegramUser(ctx, 1, 2, "operator:test", "test access restriction")
	require.NoError(t, err)
	require.Equal(t, first, replay)
	var actor, reason string
	var audits, identities int
	require.NoError(
		t,
		s.Pool().
			QueryRow(ctx, `SELECT count(*),min(actor),min(reason) FROM shuttle_user_bans`).
			Scan(&audits, &actor, &reason),
	)
	require.Equal(t, 1, audits)
	require.Equal(t, "operator:test", actor)
	require.Equal(t, "test access restriction", reason)
	require.NoError(t, s.Pool().QueryRow(ctx, `SELECT count(*) FROM shuttle_identity_audit`).Scan(&identities))
	require.Equal(t, 1, identities)
	for _, table := range []string{
		"shuttle_conversations", "shuttle_tasks", "shuttle_inbox", "shuttle_files", "shuttle_symbols",
		"shuttle_miniapp_sessions",
	} {
		var count int
		require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
	// Unsupported external flag clearing must not make an old receipt claim a live ban.
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_users SET revoked=false WHERE id=$1`, first.UserID)
	require.NoError(t, err)
	_, err = s.BanTelegramUser(ctx, 1, 2, "operator:test", "test access restriction")
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestRevokedIntakeHasNoSideEffectsAndIsBotScoped(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	ban, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test block")
	require.NoError(t, err)
	enqueued := false
	request := db.IntakeRequest{
		BotID: 1, UpdateID: 1, TelegramUserID: 2, ChatID: 2,
		Profile: json.RawMessage(`{"first_name":"Unaccepted"}`), Payload: json.RawMessage(`{}`),
		Attachments: []files.Incoming{{}}, Symbols: []symbols.Occurrence{{}},
	}
	_, _, err = s.Intake(ctx, request, func(context.Context, pgx.Tx, uuid.UUID) error { enqueued = true; return nil })
	require.ErrorIs(t, err, db.ErrUserRevoked)
	require.False(t, enqueued)
	owner, err := s.GetUser(ctx, ban.UserID)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(owner.Profile), "rejected intake also rolls back metadata")
	for _, table := range []string{
		"shuttle_conversations", "shuttle_tasks", "shuttle_inbox", "shuttle_files", "shuttle_symbols",
	} {
		var count int
		require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
	claim := miniAppClaim()
	_, err = s.ClaimMiniAppSession(ctx, claim)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	request.BotID, request.Attachments, request.Symbols = 3, nil, nil
	allowed, _, err := s.Intake(ctx, request, nil)
	require.NoError(t, err)
	require.NotEqual(t, ban.UserID, allowed.UserID)
	other, err := s.GetUser(ctx, allowed.UserID)
	require.NoError(t, err)
	require.False(t, other.Revoked)
}

func TestBanTelegramUserLinkedTargetAndExistingRevocation(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	claim := miniAppClaim()
	attempt, err := s.ClaimMiniAppSession(ctx, claim)
	require.NoError(t, err)
	activation := miniAppActivation(claim, attempt)
	_, err = s.ActivateMiniAppSession(ctx, activation)
	require.NoError(t, err)
	ban, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "linked target")
	require.NoError(t, err)
	require.Equal(t, attempt.User.ID, ban.UserID)
	owner, err := s.GetUser(ctx, ban.UserID)
	require.NoError(t, err)
	require.Equal(t, activation.Subject, owner.Subject)
	require.Equal(t, activation.Organization, owner.Organization)
	_, err = s.MiniAppSession(ctx, 1, activation.BearerHash, activation.Generation)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = s.ActivateMiniAppSession(ctx, activation)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	second, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "additional reason")
	require.NoError(t, err)
	require.True(t, second.PreviouslyRevoked)
	require.NotEqual(t, ban.ID, second.ID)
}

func TestRevokedIntakeRejectsPreviouslyAcceptedUpdate(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	first := accepted(t, s, 1)
	_, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test duplicate block")
	require.NoError(t, err)
	_, _, err = s.Intake(
		ctx,
		db.IntakeRequest{BotID: 1, UpdateID: 1, TelegramUserID: 2, ChatID: 3, Payload: json.RawMessage(`{}`)},
		nil,
	)
	require.ErrorIs(t, err, db.ErrUserRevoked)
	got, err := s.GetTask(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, first, got)
}

func TestBanTelegramUserRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	// Validation precedes any database operation.
	s := &db.Store{}
	for _, args := range []struct {
		bot, target   int64
		actor, reason string
	}{
		{0, 2, "operator", "reason"}, {1, 0, "operator", "reason"}, {1, 1 << 52, "operator", "reason"},
		{1, 2, " ", "reason"}, {1, 2, "operator", " "}, {1, 2, "operator", "\t\n"},
	} {
		_, err := s.BanTelegramUser(t.Context(), args.bot, args.target, args.actor, args.reason)
		require.ErrorIs(t, err, db.ErrUserBanInvalid)
	}
}

func TestBanTelegramUserAuditFailureRollsBackPlaceholderAndRevocation(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	_, err := s.Pool().Exec(ctx, `ALTER TABLE shuttle_user_bans ADD CONSTRAINT reject_test_ban CHECK (false) NOT VALID`)
	require.NoError(t, err)
	_, err = s.BanTelegramUser(ctx, 1, 2, "operator:test", "test audit failure")
	require.Error(t, err)
	for _, table := range []string{"shuttle_users", "shuttle_user_bans", "shuttle_identity_audit"} {
		var count int
		require.NoError(t, s.Pool().QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count, table)
	}
}

func TestBanTelegramUserAuditSurvivesMigrationRollback(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	first, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test durable audit")
	require.NoError(t, err)
	history := migrationHistory(t, s)
	_, err = baselineProvider(t, s).ApplyVersion(ctx, 77, false)
	require.ErrorContains(t, err, "user ban audit must be preserved")
	require.Equal(t, history, migrationHistory(t, s))
	replay, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test durable audit")
	require.NoError(t, err)
	require.Equal(t, first, replay)
}

func TestBanTelegramUserAuditRejectsBlankReasonAndActor(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	ban, err := s.BanTelegramUser(ctx, 1, 2, "operator:test", "test reason constraint")
	require.NoError(t, err)
	for _, args := range []struct{ actor, reason string }{{"operator:test", " "}, {" ", "test reason"}} {
		_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_user_bans
(id,user_id,actor,reason,request_hash,previously_revoked) VALUES($1,$2,$3,$4,$5,true)`,
			uuid.New(), ban.UserID, args.actor, args.reason, make([]byte, 32))
		require.ErrorContains(t, err, "check constraint")
	}
	var count int
	require.NoError(t, s.Pool().QueryRow(ctx, `SELECT count(*) FROM shuttle_user_bans`).Scan(&count))
	require.Equal(t, 1, count)
}
