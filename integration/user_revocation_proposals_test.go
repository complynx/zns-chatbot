package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func revocationArguments(target db.User) db.UserRevocationArguments {
	return db.UserRevocationArguments{
		UserID:          target.ID,
		ExpectedSubject: target.Subject,
		ExpectedRevoked: target.Revoked,
	}
}

func TestUserRevocationApprovalReplaysOriginalReceiptAfterRestartAndBindingChange(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	ctx := t.Context()
	args := revocationArguments(target)
	prep, err := h.store.PrepareUserRevocation(ctx, scope, "revoke", args)
	require.NoError(t, err)
	repeated, err := h.store.PrepareUserRevocation(ctx, scope, "revoke", args)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
	require.Zero(t, revocationAuditCount(t, h, target.ID))
	_, err = h.store.UserRevocationReceipt(ctx, scope, "revoke")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	type outcome struct {
		result db.UserRevocationResult
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			r, e := h.store.ConfirmUserRevocation(ctx, scope, "revoke", args, prep.ID)
			results <- outcome{r, e}
		}()
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Equal(t, one.result, two.result)
	require.Equal(t, db.UserRevocationResult{UserID: target.ID, Subject: target.Subject, Revoked: true}, one.result)
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_users SET zitadel_subject='rebound-subject' WHERE id=$1", target.ID)
	require.NoError(t, err)
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	receipt, err := reopened.UserRevocationReceipt(ctx, scope, "revoke")
	require.NoError(t, err)
	require.Equal(t, one.result, *receipt)
	replay, err := reopened.ConfirmUserRevocation(ctx, scope, "revoke", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, one.result, replay)
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
	current, err := reopened.GetUser(ctx, target.ID)
	require.NoError(t, err)
	args = revocationArguments(current)
	next, err := reopened.PrepareUserRevocation(ctx, scope, "fresh-revoke", args)
	require.NoError(t, err)
	_, err = reopened.ConfirmUserRevocation(ctx, scope, "fresh-revoke", args, next.ID)
	require.NoError(t, err)
	require.EqualValues(t, 2, revocationAuditCount(t, h, target.ID))
}

func TestUserRevocationRejectsChangedExpiredAndForeignApprovals(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"subject", "state", "expired", "arguments", "proposal", "foreign-bot", "foreign-org", "actor-subject", "actor-revoked", "role"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, scope, target := revocationFixture(t)
			ctx := t.Context()
			args := revocationArguments(target)
			prep, err := h.store.PrepareUserRevocation(ctx, scope, "revoke", args)
			require.NoError(t, err)
			expected := db.ErrConflict
			switch name {
			case "subject":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_users SET zitadel_subject='changed' WHERE id=$1", target.ID)
			case "state":
				_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", target.ID)
			case "expired":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_user_revocation_proposals SET expires_at=clock_timestamp()-interval '1 second'")
			case "arguments":
				args.ExpectedSubject = "unapproved"
			case "proposal":
				prep.ID = uuid.New()
			case "foreign-bot":
				scope.BotID++
				expected = db.ErrAdminDenied
			case "foreign-org":
				scope.Organization = "other"
				expected = db.ErrAdminDenied
			case "actor-subject":
				scope.Subject = "other"
				expected = db.ErrAdminDenied
			case "actor-revoked":
				_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
				expected = db.ErrAdminDenied
			case "role":
				scope.GlobalAdmin = false
				expected = db.ErrAdminDenied
			}
			require.NoError(t, err)
			_, err = h.store.ConfirmUserRevocation(ctx, scope, "revoke", args, prep.ID)
			require.ErrorIs(t, err, expected)
			require.Zero(t, revocationAuditCount(t, h, target.ID))
		})
	}
}

func TestUserRevocationSelfReceiptRequiresAnotherActiveAdministrator(t *testing.T) {
	t.Parallel()
	h, scope, observer := revocationFixture(t)
	ctx := t.Context()
	self, err := h.store.GetUser(ctx, scope.UserID)
	require.NoError(t, err)
	args := revocationArguments(self)
	prep, err := h.store.PrepareUserRevocation(ctx, scope, "self-revoke", args)
	require.NoError(t, err)
	require.Contains(t, prep.Description, "your own account")
	result, err := h.store.ConfirmUserRevocation(ctx, scope, "self-revoke", args, prep.ID)
	require.NoError(t, err)
	require.True(t, result.Revoked)
	_, err = h.store.UserRevocationReceipt(ctx, scope, "self-revoke")
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = h.store.ConfirmUserRevocation(ctx, scope, "self-revoke", args, prep.ID)
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = h.store.UserRevocationDiagnostic(ctx, scope, prep.ID)
	require.ErrorIs(t, err, db.ErrAdminDenied)
	other := scope
	other.UserID, other.Subject = observer.ID, observer.Subject
	diagnostic, err := h.store.UserRevocationDiagnostic(ctx, other, prep.ID)
	require.NoError(t, err)
	require.Equal(t, scope.Subject, diagnostic.Actor)
	require.Equal(t, "self-revoke", diagnostic.OperationID)
	require.Equal(t, result, *diagnostic.Result)
	_, err = h.store.ConfirmUserRevocation(ctx, other, "self-revoke", args, prep.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.EqualValues(t, 1, revocationAuditCount(t, h, self.ID))
}

func TestUserRevocationReciprocalAndConcurrentSelfRequestsDoNotDeadlock(t *testing.T) {
	t.Parallel()
	for _, self := range []bool{false, true} {
		t.Run(map[bool]string{false: "reciprocal", true: "self"}[self], func(t *testing.T) {
			t.Parallel()
			h, one, second := revocationFixture(t)
			two := one
			two.UserID, two.Subject = second.ID, second.Subject
			oneTarget, twoTarget := two.UserID, one.UserID
			if self {
				oneTarget, twoTarget = one.UserID, one.UserID
				two = one
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			start := make(chan struct{})
			results := make(chan error, 2)
			go func() { <-start; _, err := h.store.RevokeScopedUser(ctx, one, oneTarget); results <- err }()
			go func() { <-start; _, err := h.store.RevokeScopedUser(ctx, two, twoTarget); results <- err }()
			close(start)
			first, last := <-results, <-results
			if first == nil {
				require.ErrorIs(t, last, db.ErrAdminDenied)
			} else {
				require.ErrorIs(t, first, db.ErrAdminDenied)
				require.NoError(t, last)
			}
			require.EqualValues(t, 1, revocationAuditCount(t, h, one.UserID)+revocationAuditCount(t, h, second.ID))
		})
	}
}

func TestUserRevocationResultFailureRollsBackRevocationAndAudit(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	ctx := t.Context()
	args := revocationArguments(target)
	prep, err := h.store.PrepareUserRevocation(ctx, scope, "revoke", args)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, "CREATE FUNCTION fail_revocation_result() RETURNS trigger LANGUAGE plpgsq" +
"l AS $$ BEGIN RAISE EXCEPTION 'fixture receipt failure'; END $$")
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, "CREATE TRIGGER fail_revocation_result BEFORE UPDATE ON shuttle_user_revo" +
"cation_proposals FOR EACH ROW EXECUTE FUNCTION fail_revocation_result()")
	require.NoError(t, err)
	_, err = h.store.ConfirmUserRevocation(ctx, scope, "revoke", args, prep.ID)
	require.Error(t, err)
	current, err := h.store.GetUser(ctx, target.ID)
	require.NoError(t, err)
	require.False(t, current.Revoked)
	require.Zero(t, revocationAuditCount(t, h, target.ID))
	_, err = h.store.UserRevocationReceipt(ctx, scope, "revoke")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestUserRevocationExpiresWhileWaitingForTargetLock(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	ctx := t.Context()
	args := revocationArguments(target)
	prep, err := h.store.PrepareUserRevocation(ctx, scope, "blocked", args)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(ctx, "UPDATE shuttle_user_revocation_proposals SET expires_at=clock_timestamp()+interval '1 second'")
	require.NoError(t, err)
	holder, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
	var pid int32
	require.NoError(
		t,
		holder.QueryRow(ctx, "SELECT pg_backend_pid() FROM shuttle_users WHERE id=$1 FOR UPDATE", target.ID).Scan(&pid),
	)
	done := make(chan error, 1)
	go func() {
		_, e := h.store.ConfirmUserRevocation(ctx, scope, "blocked", args, prep.ID)
		done <- e
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
			QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM shuttle_user_revocation_proposals").
			Scan(&expired)
		return e == nil && expired
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, holder.Rollback(ctx))
	require.ErrorIs(t, <-done, db.ErrConflict)
	require.Zero(t, revocationAuditCount(t, h, target.ID))
}

func TestUserRevocationSupportsEmptyBindingAndRejectsTargetsOutsideDeployment(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	ctx := t.Context()
	_, err := h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET zitadel_subject='' WHERE id=$1", target.ID)
	require.NoError(t, err)
	args := db.UserRevocationArguments{UserID: target.ID}
	prep, err := h.store.PrepareUserRevocation(ctx, scope, "unbound", args)
	require.NoError(t, err)
	result, err := h.store.ConfirmUserRevocation(ctx, scope, "unbound", args, prep.ID)
	require.NoError(t, err)
	require.Empty(t, result.Subject)
	require.True(t, result.Revoked)
	foreign := scopedAccount(t, h, scope.BotID+1, 503, "foreign-target")
	_, err = h.store.PrepareUserRevocation(ctx, scope, "foreign", revocationArguments(foreign))
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.RevokeScopedUser(ctx, scope, foreign.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Zero(t, revocationAuditCount(t, h, foreign.ID))
	_, err = h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET zitadel_org='other' WHERE id=$1", target.ID)
	require.NoError(t, err)
	_, err = h.store.RevokeScopedUser(ctx, scope, target.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
}
