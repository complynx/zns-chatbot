package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func settingsChangeFixture(t *testing.T) (*harness, db.AdminScope, db.SettingsChangeArguments) {
	t.Helper()
	h := prepareHarness(t, newPeers(t))
	t.Cleanup(func() { h.stop(t) })
	intake, _, err := h.store.Intake(t.Context(), db.IntakeRequest{
		BotID: 123, TelegramUserID: 501, ChatID: 501, UpdateID: 501, Payload: json.RawMessage(`{}`),
	}, nil)
	require.NoError(t, err)
	_, err = h.store.BindSubject(t.Context(), intake.UserID, "settings-admin", "org")
	require.NoError(t, err)
	before, err := h.store.Settings(t.Context())
	require.NoError(t, err)
	document := before.Document
	document.DefaultLanguage = "ru"
	return h, db.AdminScope{
			BotID:        123,
			Organization: "org",
			Subject:      "settings-admin",
			UserID:       intake.UserID,
			GlobalAdmin:  true,
		},
		db.SettingsChangeArguments{
			ExpectedVersion: before.Version,
			Document:        document,
		}
}

func settingsAuditCount(t *testing.T, h *harness) int64 {
	t.Helper()
	var n int64
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_settings_audit").Scan(&n))
	return n
}

func TestSettingsChangeConcurrentConfirmationAndOriginalReceiptAfterNewerSettings(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	before, err := h.store.Settings(ctx)
	require.NoError(t, err)
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "settings-concurrent", args)
	require.NoError(t, err)
	require.Contains(t, prep.Description, "application-wide")
	require.LessOrEqual(t, len(prep.Description), 4000, "Loom approval descriptions have a 4000-byte bound")
	require.Contains(t, prep.Description, `"massage_notify_client_prior_long_seconds": 3600`)
	require.Contains(t, prep.Description, `"massage_notify_client_prior_seconds": 600`)
	require.Contains(t, prep.Description, `"name_expectation_seconds": 7200`)
	require.Contains(t, prep.Description, `"mini_app_session_seconds": 900`)
	require.Contains(t, prep.Description, "after application restart")
	require.Zero(t, prep.Arguments.Document.NameExpectationSeconds)
	repeated, err := h.store.PrepareSettingsChange(ctx, scope, "settings-concurrent", args)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
	unchanged, err := h.store.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, before, unchanged)
	require.Zero(t, settingsAuditCount(t, h))
	_, err = h.store.SettingsChangeReceipt(ctx, scope, "settings-concurrent")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	type outcome struct {
		result db.VersionedSettings
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			r, e := h.store.ConfirmSettingsChange(ctx, scope, "settings-concurrent", args, prep.ID)
			results <- outcome{r, e}
		}()
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Equal(t, one.result, two.result)
	require.Equal(t, args.Document, one.result.Document)
	require.Equal(t, before.Version+1, one.result.Version)
	require.EqualValues(t, 1, settingsAuditCount(t, h))
	newerDoc := one.result.Document
	newerDoc.AuthCacheTTLSeconds = 120
	newer, err := h.store.CompareAndSwapSettings(ctx, one.result.Version, newerDoc, "operator")
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_settings_change_proposals SET expires_at=clock_timestamp()-interval '1 second'")
	require.NoError(t, err)
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	receipt, err := reopened.SettingsChangeReceipt(ctx, scope, "settings-concurrent")
	require.NoError(t, err)
	require.Equal(t, one.result, *receipt)
	replay, err := reopened.ConfirmSettingsChange(ctx, scope, "settings-concurrent", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, one.result, replay)
	current, err := reopened.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, newer, current)
	require.EqualValues(t, 2, settingsAuditCount(t, h))
}

func TestSettingsChangeRejectsInvalidChangedStaleAndExpiredApprovals(t *testing.T) {
	t.Parallel()
	for _, test := range []string{"changed-document", "changed-version", "wrong-proposal", "expired", "stale", "invalid-language", "invalid-ttl", "invalid-miniapp", "invalid-reminder-negative", "invalid-reminder-overflow", "missing-version"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			h, scope, args := settingsChangeFixture(t)
			ctx := t.Context()
			prep, err := h.store.PrepareSettingsChange(ctx, scope, "settings-reject", args)
			require.NoError(t, err)
			proposal := prep.ID
			switch test {
			case "changed-document":
				args.Document.AuthCacheTTLSeconds = 60
			case "changed-version":
				args.ExpectedVersion++
			case "wrong-proposal":
				proposal = uuid.New()
			case "expired":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_settings_change_proposals SET expires_at=clock_timestamp()-interval '1 second'")
				require.NoError(t, err)
			case "stale":
				_, err = h.store.CompareAndSwapSettings(ctx, args.ExpectedVersion, args.Document, "operator")
				require.NoError(t, err)
			case "invalid-language":
				args.Document.DefaultLanguage = "unknown"
			case "invalid-ttl":
				args.Document.AuthCacheTTLSeconds = 301
			case "invalid-reminder-negative":
				args.Document.MassageNotifyClientPriorLongSeconds = -1
			case "invalid-reminder-overflow":
				args.Document.MassageNotifyClientPriorSeconds = db.MaxMassageReminderLeadSeconds + 1
			case "invalid-miniapp":
				args.Document.MiniAppSessionSeconds = 1
			case "missing-version":
				args.ExpectedVersion = 0
			}
			before, err := h.store.Settings(ctx)
			require.NoError(t, err)
			audits := settingsAuditCount(t, h)
			_, err = h.store.ConfirmSettingsChange(ctx, scope, "settings-reject", args, proposal)
			require.Error(t, err)
			current, err := h.store.Settings(ctx)
			require.NoError(t, err)
			require.Equal(t, before, current)
			require.Equal(t, audits, settingsAuditCount(t, h))
			_, err = h.store.SettingsChangeReceipt(ctx, scope, "settings-reject")
			require.ErrorIs(t, err, pgx.ErrNoRows)
		})
	}
}

func TestSettingsChangeScopesReceiptsAndDeniesRevokedActors(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "private-settings", args)
	require.NoError(t, err)
	otherTask := closureIntake(t, h, 123, 502, 502)
	_, err = h.store.BindSubject(ctx, otherTask.UserID, "second-settings-admin", "org")
	require.NoError(t, err)
	other := scope
	other.Subject, other.UserID = "second-settings-admin", otherTask.UserID
	_, err = h.store.SettingsChangeReceipt(ctx, other, "private-settings")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.ConfirmSettingsChange(ctx, other, "private-settings", args, prep.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	second, err := h.store.PrepareSettingsChange(ctx, other, "private-settings", args)
	require.NoError(t, err)
	require.NotEqual(t, prep.ID, second.ID)
	for _, change := range []func(*db.AdminScope){
		func(s *db.AdminScope) { s.GlobalAdmin = false }, func(s *db.AdminScope) { s.BotID++ },
		func(s *db.AdminScope) { s.Organization = "foreign" }, func(s *db.AdminScope) { s.UserID = uuid.New() },
		func(s *db.AdminScope) { s.Subject = "mismatch" },
	} {
		invalid := scope
		change(&invalid)
		_, err = h.store.ConfirmSettingsChange(ctx, invalid, "private-settings", args, prep.ID)
		require.ErrorIs(t, err, db.ErrAdminDenied)
	}
	_, err = h.store.ConfirmSettingsChange(ctx, scope, "private-settings", args, prep.ID)
	require.NoError(t, err)
	_, err = h.store.SetUserRevoked(ctx, scope.UserID, "operator")
	require.NoError(t, err)
	_, err = h.store.SettingsChangeReceipt(ctx, scope, "private-settings")
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = h.store.ConfirmSettingsChange(ctx, scope, "private-settings", args, prep.ID)
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = h.store.SettingsChangeReceipt(ctx, other, "private-settings")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestSettingsChangeResultFailureRollsBackSettingsAndAudit(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	before, err := h.store.Settings(ctx)
	require.NoError(t, err)
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "rollback-settings", args)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(ctx, `CREATE FUNCTION reject_settings_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'synthetic settings result failure'; END $$;
CREATE TRIGGER reject_settings_result BEFORE UPDATE ON shuttle_settings_change_proposals
FOR EACH ROW EXECUTE FUNCTION reject_settings_result()`)
	require.NoError(t, err)
	_, err = h.store.ConfirmSettingsChange(ctx, scope, "rollback-settings", args, prep.ID)
	require.Error(t, err)
	current, err := h.store.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, before, current)
	require.Zero(t, settingsAuditCount(t, h))
	_, err = h.store.SettingsChangeReceipt(ctx, scope, "rollback-settings")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.Pool().Exec(ctx, `DROP TRIGGER reject_settings_result ON shuttle_settings_change_proposals;
DROP FUNCTION reject_settings_result()`)
	require.NoError(t, err)
	_, err = h.store.ConfirmSettingsChange(ctx, scope, "rollback-settings", args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, settingsAuditCount(t, h))
}

func TestSettingsChangeApprovalExpiresWhileWaitingForSettingsLock(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "blocked-settings", args)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_settings_change_proposals SET expires_at=clock_timestamp()+interval '1 second'")
	require.NoError(t, err)
	holder, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	var pid int32
	require.NoError(
		t,
		holder.QueryRow(ctx, "SELECT pg_backend_pid() FROM shuttle_settings WHERE id=true FOR UPDATE").Scan(&pid),
	)
	result := make(chan error, 1)
	go func() {
		_, e := h.store.ConfirmSettingsChange(ctx, scope, "blocked-settings", args, prep.ID)
		result <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := h.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", pid).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		e := h.store.Pool().
			QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM shuttle_settings_change_proposals").
			Scan(&expired)
		return e == nil && expired
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, holder.Rollback(ctx))
	require.ErrorIs(t, <-result, db.ErrConflict)
	current, err := h.store.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, args.ExpectedVersion, current.Version)
	require.Zero(t, settingsAuditCount(t, h))
}

func TestSettingsChangeAndOperatorCASHaveOneWinner(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "operator-race", args)
	require.NoError(t, err)
	operatorDoc := args.Document
	operatorDoc.AuthCacheTTLSeconds = 60
	results := make(chan error, 2)
	go func() {
		_, e := h.store.ConfirmSettingsChange(ctx, scope, "operator-race", args, prep.ID)
		results <- e
	}()
	go func() {
		_, e := h.store.CompareAndSwapSettings(ctx, args.ExpectedVersion, operatorDoc, "operator")
		results <- e
	}()
	one, two := <-results, <-results
	if one == nil {
		require.ErrorIs(t, two, db.ErrConflict)
	} else {
		require.ErrorIs(t, one, db.ErrConflict)
		require.NoError(t, two)
	}
	current, err := h.store.Settings(ctx)
	require.NoError(t, err)
	require.Equal(t, args.ExpectedVersion+1, current.Version)
	require.EqualValues(t, 1, settingsAuditCount(t, h))
	receipt, err := h.store.SettingsChangeReceipt(ctx, scope, "operator-race")
	if current.Document == args.Document {
		require.NoError(t, err)
		require.Equal(t, current, *receipt)
	} else {
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.Equal(t, operatorDoc, current.Document)
	}
}

func TestSettingsChangeMigrationAllowsEmptyRollbackAndPreservesApprovalAudit(t *testing.T) {
	t.Parallel()
	h, scope, args := settingsChangeFixture(t)
	ctx := t.Context()
	driver := stdlib.OpenDBFromPool(h.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 29, false)
	require.NoError(t, err)
	operator, err := h.store.CompareAndSwapSettings(ctx, args.ExpectedVersion, args.Document, "operator")
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 29, true)
	require.NoError(t, err)
	args.ExpectedVersion = operator.Version
	prep, err := h.store.PrepareSettingsChange(ctx, scope, "retained-settings-approval", args)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 29, false)
	require.ErrorContains(t, err, "settings approval and result history must be exported")
	repeated, err := h.store.PrepareSettingsChange(ctx, scope, "retained-settings-approval", args)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
	require.EqualValues(t, 1, settingsAuditCount(t, h))
}
