package integration_test

import (
	"context"
	"crypto/sha256"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type miniAppAssignmentFixture struct {
	h         *harness
	scope     db.EventScope
	actor     string
	recipient uuid.UUID
	args      db.PassAssignment
	session   db.MiniAppAssignmentSession
}

func setupMiniAppAssignment(t *testing.T) miniAppAssignmentFixture {
	t.Helper()
	h := setup(t, newPeers(t))
	adminID, token := registrationUser(t, h, 98601, true)
	recipient, owner := registrationUser(t, h, 98602, false)
	event := registrationEvent(t, h, token)
	entry := registerForEvent(t, h, owner, event.ID, "assignment-native-join", registrations.Leader)
	admin, err := h.store.GetUser(t.Context(), adminID)
	require.NoError(t, err)
	scope := db.EventScope{BotID: 123, Organization: "org"}
	auth := nativePaymentSession(
		t,
		paymentFixture{H: h},
		db.PaymentScope{BotID: scope.BotID, Organization: scope.Organization, UserID: adminID, Subject: admin.Subject},
		98601,
	)
	return miniAppAssignmentFixture{
		h:         h,
		scope:     scope,
		actor:     admin.Subject,
		recipient: recipient,
		args: db.PassAssignment{
			EventID:              event.ID,
			RegistrationID:       entry.ID,
			ExpectedEventVersion: event.Version,
			ExpectedVersion:      entry.Version,
		},
		session: db.MiniAppAssignmentSession{
			UserID:             adminID,
			Bearer:             auth.Bearer,
			Generation:         auth.Generation,
			AuthorityUnchanged: func() bool { return true },
		},
	}
}

func TestMiniAppAssignmentNativeAndMixedPathsShareCanonicalReceipt(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			f := setupMiniAppAssignment(t)
			ctx := t.Context()
			operation := "original-assignment"
			var prep db.AssignmentPreparation
			var err error
			if path == "agent_to_native" {
				prep, err = f.h.store.PrepareAssignment(ctx, f.scope, f.actor, operation, f.args)
			} else {
				prep, err = f.h.store.PrepareMiniAppAssignment(
					ctx,
					f.scope,
					f.actor,
					operation,
					f.args,
					uuid.Nil,
					f.session,
				)
			}
			require.NoError(t, err)
			require.NotEmpty(t, prep.Description)
			require.NotEmpty(t, prep.Quotes)
			var effects int
			require.NoError(t, f.h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&effects))
			require.Zero(t, effects, "Preparing a proposal must not assign a pass")
			switch path {
			case "native_to_agent":
				operation = "agent-envelope"
				adopted, e := f.h.store.AdoptAssignment(ctx, f.scope, f.actor, operation, f.args, prep.ID)
				require.NoError(t, e)
				require.Equal(t, prep, adopted, "Handoff must preserve the quoted terms and expiry")
			case "agent_to_native":
				operation = "native-envelope"
				adopted, e := f.h.store.PrepareMiniAppAssignment(
					ctx,
					f.scope,
					f.actor,
					operation,
					f.args,
					prep.ID,
					f.session,
				)
				require.NoError(t, e)
				require.Equal(t, prep, adopted)
			}
			var assigned registrations.Registration
			if path == "native_to_agent" {
				assigned, err = f.h.store.ConfirmAssignment(ctx, f.scope, f.actor, operation, f.args, prep.ID)
			} else {
				assigned, err = f.h.store.ConfirmMiniAppAssignment(
					ctx,
					f.scope,
					f.actor,
					operation,
					f.args,
					prep.ID,
					f.session,
				)
			}
			require.NoError(t, err)
			require.Equal(t, "assigned", assigned.State)
			require.NotNil(t, assigned.Pass)
			reopened, err := db.Open(ctx, f.h.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			replay, err := reopened.ConfirmMiniAppAssignment(
				ctx,
				f.scope,
				f.actor,
				operation,
				f.args,
				prep.ID,
				f.session,
			)
			require.NoError(t, err)
			require.Equal(t, assigned, replay)
			for _, key := range []string{"original-assignment", operation} {
				receipt, e := reopened.AssignmentOperation(ctx, f.scope, f.actor, key)
				require.NoError(t, e)
				require.Equal(t, assigned, receipt)
			}
			require.NoError(t, reopened.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&effects))
			require.Equal(t, 1, effects)
			require.NoError(
				t,
				reopened.Pool().
					QueryRow(ctx, "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", f.actor).
					Scan(&effects),
			)
			require.Equal(t, 1, effects)
		})
	}
}

func assignmentSessionFailure(t *testing.T, f miniAppAssignmentFixture, failure string) db.MiniAppAssignmentSession {
	t.Helper()
	ctx := t.Context()
	session := f.session
	switch failure {
	case "foreign_session":
		owner, e := f.h.store.GetUser(ctx, f.recipient)
		require.NoError(t, e)
		foreign := nativePaymentSession(
			t,
			paymentFixture{H: f.h},
			db.PaymentScope{
				BotID:        f.scope.BotID,
				Organization: f.scope.Organization,
				UserID:       f.recipient,
				Subject:      owner.Subject,
			},
			98602,
		)
		session.Bearer, session.Generation = foreign.Bearer, foreign.Generation
	case "generation":
		session.Generation = sha256.Sum256([]byte("different-generation"))
	case "logout":
		require.NoError(
			t,
			f.h.store.LogoutMiniAppSession(ctx, f.scope.BotID, session.Bearer, session.Generation),
		)
	case "expired":
		_, e := f.h.store.Pool().
			Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE bearer_hash=$1", session.Bearer[:])
		require.NoError(t, e)
	case "revoked_actor":
		require.NoError(t, f.h.store.RevokeUser(ctx, session.UserID, true))
	case "nil_authority":
		session.AuthorityUnchanged = nil
	case "invalidated_authority":
		session.AuthorityUnchanged = func() bool { return false }
	}
	return session
}

func requireAssignmentEffectsForPhase(t *testing.T, f miniAppAssignmentFixture, completed bool) {
	t.Helper()
	ctx := t.Context()
	var passes, receipts int
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&passes),
	)
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", f.actor).
			Scan(&receipts),
	)
	expected := 0
	if completed {
		expected = 1
	}
	require.Equal(t, expected, passes)
	require.Equal(t, expected, receipts)
}

func TestMiniAppAssignmentRejectsInvalidAuthorityBeforePreparationAndReceiptReplay(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		phase := "before_preparation"
		if completed {
			phase = "completed_receipt"
		}
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []string{"foreign_session", "generation", "logout", "expired", "revoked_actor", "nil_authority", "invalidated_authority"} {
				t.Run(failure, func(t *testing.T) {
					t.Parallel()
					f := setupMiniAppAssignment(t)
					ctx := t.Context()
					operation := "session-fenced-assignment"
					var proposal uuid.UUID
					if completed {
						prep, e := f.h.store.PrepareMiniAppAssignment(
							ctx,
							f.scope,
							f.actor,
							operation,
							f.args,
							uuid.Nil,
							f.session,
						)
						require.NoError(t, e)
						proposal = prep.ID
						_, e = f.h.store.ConfirmMiniAppAssignment(
							ctx,
							f.scope,
							f.actor,
							operation,
							f.args,
							proposal,
							f.session,
						)
						require.NoError(t, e)
					}
					session := assignmentSessionFailure(t, f, failure)
					_, err := f.h.store.PrepareMiniAppAssignment(
						ctx,
						f.scope,
						f.actor,
						operation,
						f.args,
						uuid.Nil,
						session,
					)
					require.Error(t, err)
					if failure == "nil_authority" || failure == "invalidated_authority" {
						require.ErrorIs(t, err, payments.ErrDenied)
					}
					_, err = f.h.store.ConfirmMiniAppAssignment(
						ctx,
						f.scope,
						f.actor,
						operation,
						f.args,
						proposal,
						session,
					)
					require.Error(t, err)
					requireAssignmentEffectsForPhase(t, f, completed)
				})
			}
		})
	}
}

func TestMiniAppAssignmentRequiresExplicitProposal(t *testing.T) {
	t.Parallel()
	f := setupMiniAppAssignment(t)
	prep, err := f.h.store.PrepareMiniAppAssignment(
		t.Context(),
		f.scope,
		f.actor,
		"explicit-consent",
		f.args,
		uuid.Nil,
		f.session,
	)
	require.NoError(t, err)
	_, err = f.h.store.ConfirmMiniAppAssignment(
		t.Context(),
		f.scope,
		f.actor,
		"explicit-consent",
		f.args,
		uuid.Nil,
		f.session,
	)
	require.Error(t, err)
	current, err := f.h.store.Registration(t.Context(), f.scope, f.args.EventID, f.recipient)
	require.NoError(t, err)
	require.Equal(t, "waitlisted", current.State)
	require.Nil(t, current.Pass)
	_, err = f.h.store.ConfirmMiniAppAssignment(
		t.Context(),
		f.scope,
		f.actor,
		"explicit-consent",
		f.args,
		prep.ID,
		f.session,
	)
	require.NoError(t, err)
}

func TestMiniAppAssignmentRechecksAuthorityAfterRecipientLockWait(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"invalidated_authority", "expired_session"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f := setupMiniAppAssignment(t)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			var unchanged atomic.Bool
			unchanged.Store(true)
			f.session.AuthorityUnchanged = unchanged.Load
			prep, err := f.h.store.PrepareMiniAppAssignment(
				ctx,
				f.scope,
				f.actor,
				"blocked-assignment",
				f.args,
				uuid.Nil,
				f.session,
			)
			require.NoError(t, err)
			var deadline time.Time
			if failure == "expired_session" {
				err = f.h.store.Pool().
					QueryRow(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE bearer_hash=$1 RETURNING expires_at", f.session.Bearer[:]).
					Scan(&deadline)
				require.NoError(t, err)
			}
			blocker, err := f.h.store.Pool().Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = blocker.Rollback(context.Background()) }()
			_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", f.recipient)
			require.NoError(t, err)
			finished := make(chan error, 1)
			go func() {
				_, e := f.h.store.ConfirmMiniAppAssignment(
					ctx,
					f.scope,
					f.actor,
					"blocked-assignment",
					f.args,
					prep.ID,
					f.session,
				)
				finished <- e
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				e := f.h.store.Pool().
					QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocker.Conn().PgConn().PID()).
					Scan(&waiting)
				return e == nil && waiting
			}, 3*time.Second, 10*time.Millisecond)
			if failure == "invalidated_authority" {
				unchanged.Store(false)
			} else {
				require.Eventually(t, func() bool {
					var expired bool
					e := f.h.store.Pool().
						QueryRow(ctx, "SELECT clock_timestamp()>$1::timestamptz", deadline).
						Scan(&expired)
					return e == nil && expired
				}, 3*time.Second, 10*time.Millisecond)
			}
			require.NoError(t, blocker.Commit(ctx))
			select {
			case err = <-finished:
				require.Error(t, err)
			case <-ctx.Done():
				t.Fatal("Assignment did not finish after recipient lock release")
			}
			var effects int
			require.NoError(t, f.h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&effects))
			require.Zero(t, effects)
			require.NoError(
				t,
				f.h.store.Pool().
					QueryRow(ctx, "SELECT count(*) FROM shuttle_registration_operations WHERE actor=$1", f.actor).
					Scan(&effects),
			)
			require.Zero(t, effects)
			current, e := f.h.store.Registration(ctx, f.scope, f.args.EventID, f.recipient)
			require.NoError(t, e)
			require.Equal(t, "waitlisted", current.State)
		})
	}
}
