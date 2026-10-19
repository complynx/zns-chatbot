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
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/migrations"
)

func eventChangeFixture(t *testing.T) (*harness, db.AdminScope, db.EventChange) {
	t.Helper()
	h := prepareHarness(t, newPeers(t))
	intake, _, err := h.store.Intake(t.Context(), db.IntakeRequest{
		BotID: 123, TelegramUserID: 601, ChatID: 601, UpdateID: 601, Payload: json.RawMessage(`{}`),
	}, nil)
	require.NoError(t, err)
	_, err = h.store.BindSubject(t.Context(), intake.UserID, "event-admin", "org")
	require.NoError(t, err)
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	return h, db.AdminScope{
		BotID: 123, Organization: "org", Subject: "event-admin", UserID: intake.UserID, GlobalAdmin: true,
	}, db.EventChange{ID: uuid.New(), Document: events.Document{
		Name: " Grodno ", Timezone: "Europe/Minsk", StartsAt: start, EndsAt: start.Add(48 * time.Hour),
		Modules: []string{"registrations", "orders"},
		Registration: &registrations.Rules{
			LeaderCapacity: 10, FollowerCapacity: 10, PriceMinor: 12345, Currency: "BYN", CurrencyScale: 2,
			ClosesAt: start, PaymentWindowSeconds: 3600,
		},
		Deposit: &registrations.DepositPolicy{MinimumMinor: 1000, MinimumBPS: 1000},
	}}
}

func TestEventChangeCreatesReplacesPoliciesAndPreservesOriginalReceipt(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	ctx := t.Context()
	eventScope := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	prep, err := h.store.PrepareEventChange(ctx, scope, "event-create", args)
	require.NoError(t, err)
	require.Contains(t, prep.Description, "registration/deposit removes")
	require.Contains(t, prep.Description, "starts_at_local")
	require.Equal(t, "Grodno", prep.Arguments.Document.Name)
	_, err = h.store.Event(ctx, eventScope, args.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.EventChangeReceipt(ctx, scope, "event-create")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	repeated, err := h.store.PrepareEventChange(ctx, scope, "event-create", prep.Arguments)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
	created, err := h.store.ConfirmEventChange(ctx, scope, "event-create", args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, created.Version)
	require.Equal(t, prep.Arguments.Document, created.Document)
	// An operator may use the same textual operation, but neither receipt adopts the other.
	_, err = h.store.EventOperation(ctx, eventScope, scope.Subject, "event-create")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	update := db.EventChange{ID: args.ID, ExpectedVersion: created.Version, Document: created.Document}
	update.Document.Name = "Updated"
	update.Document.Registration, update.Document.Deposit = nil, nil
	operator, err := h.store.ChangeEvent(ctx, eventScope, scope.Subject, "event-create", update)
	require.NoError(t, err)
	require.EqualValues(t, 2, operator.Version)
	require.Nil(t, operator.Document.Registration)
	require.Nil(t, operator.Document.Deposit)
	reopened, err := db.Open(ctx, h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	receipt, err := reopened.EventChangeReceipt(ctx, scope, "event-create")
	require.NoError(t, err)
	require.Equal(t, created, *receipt)
	replay, err := reopened.ConfirmEventChange(ctx, scope, "event-create", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, created, replay)
	update.ExpectedVersion = operator.Version
	newPrep, err := reopened.PrepareEventChange(ctx, scope, "event-replace", update)
	require.NoError(t, err)
	result, err := reopened.ConfirmEventChange(ctx, scope, "event-replace", update, newPrep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3, result.Version)
	require.Nil(t, result.Document.Deposit)
	require.Nil(t, result.Document.Registration)
	_, err = reopened.SetUserRevoked(ctx, scope.UserID, "fixture")
	require.NoError(t, err)
	_, err = reopened.ConfirmEventChange(ctx, scope, "event-create", args, prep.ID)
	require.ErrorIs(t, err, db.ErrAdminDenied)
	_, err = reopened.EventChangeReceipt(ctx, scope, "event-create")
	require.ErrorIs(t, err, db.ErrAdminDenied)
}

func TestEventChangeDuplicateConfirmationCommitsOneVersion(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	prep, err := h.store.PrepareEventChange(t.Context(), scope, "duplicate-event", args)
	require.NoError(t, err)
	type outcome struct {
		result events.Event
		err    error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			r, e := h.store.ConfirmEventChange(t.Context(), scope, "duplicate-event", args, prep.ID)
			results <- outcome{r, e}
		}()
	}
	one, two := <-results, <-results
	require.NoError(t, one.err)
	require.NoError(t, two.err)
	require.Equal(t, one.result, two.result)
	require.EqualValues(t, 1, one.result.Version)
}

func TestEventChangeConcurrentCreatesHaveOneWinnerAndNoOperatorAdoption(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	ctx := t.Context()
	one, err := h.store.PrepareEventChange(ctx, scope, "one", args)
	require.NoError(t, err)
	two, err := h.store.PrepareEventChange(ctx, scope, "two", args)
	require.NoError(t, err)
	results := make(chan error, 2)
	for _, attempt := range []struct {
		op string
		id uuid.UUID
	}{{"one", one.ID}, {"two", two.ID}} {
		go func() {
			_, e := h.store.ConfirmEventChange(ctx, scope, attempt.op, args, attempt.id)
			results <- e
		}()
	}
	first, second := <-results, <-results
	if first == nil {
		require.ErrorIs(t, second, db.ErrConflict)
	} else {
		require.ErrorIs(t, first, db.ErrConflict)
		require.NoError(t, second)
	}
	// A prior operator receipt must not turn an unconfirmed proposal into agent success.
	other := args
	other.ID = uuid.New()
	prep, err := h.store.PrepareEventChange(ctx, scope, "operator-first", other)
	require.NoError(t, err)
	_, err = h.store.ChangeEvent(
		ctx,
		db.EventScope{BotID: scope.BotID, Organization: scope.Organization},
		scope.Subject,
		"operator-first",
		other,
	)
	require.NoError(t, err)
	_, err = h.store.ConfirmEventChange(ctx, scope, "operator-first", other, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = h.store.EventChangeReceipt(ctx, scope, "operator-first")
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestEventChangeRejectsAlteredExpiredAndUnauthorizedOperations(t *testing.T) {
	t.Parallel()
	for _, test := range []string{"changed", "proposal", "expired", "role", "revoked", "subject", "organization"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			h, scope, args := eventChangeFixture(t)
			ctx := t.Context()
			prep, err := h.store.PrepareEventChange(ctx, scope, "reject-event", args)
			require.NoError(t, err)
			id := prep.ID
			switch test {
			case "changed":
				args.Document.Name = "Changed"
			case "proposal":
				id = uuid.New()
			case "expired":
				_, err = h.store.Pool().
					Exec(ctx, "UPDATE shuttle_event_change_proposals SET expires_at=clock_timestamp()-interval '1 second'")
				require.NoError(t, err)
			case "role":
				scope.GlobalAdmin = false
			case "revoked":
				_, err = h.store.SetUserRevoked(ctx, scope.UserID, "fixture")
				require.NoError(t, err)
			case "subject":
				scope.Subject = "another-subject"
			case "organization":
				scope.Organization = "another-org"
			}
			_, err = h.store.ConfirmEventChange(ctx, scope, "reject-event", args, id)
			require.Error(t, err)
			_, err = h.store.Event(ctx, db.EventScope{BotID: 123, Organization: "org"}, args.ID)
			require.ErrorIs(t, err, pgx.ErrNoRows)
			_, err = h.store.EventChangeReceipt(ctx, scope, "reject-event")
			require.Error(t, err)
		})
	}
}

func TestEventChangeResultFailureRollsBackEventAndPolicies(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	ctx := t.Context()
	prep, err := h.store.PrepareEventChange(ctx, scope, "rollback-event", args)
	require.NoError(t, err)
	_, err = h.store.Pool().Exec(ctx, `
CREATE FUNCTION reject_event_result() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'fixture result unavailable'; END $$;
CREATE TRIGGER reject_event_result BEFORE UPDATE ON shuttle_event_change_proposals
FOR EACH ROW EXECUTE FUNCTION reject_event_result()`)
	require.NoError(t, err)
	_, err = h.store.ConfirmEventChange(ctx, scope, "rollback-event", args, prep.ID)
	require.ErrorContains(t, err, "fixture result unavailable")
	_, err = h.store.Event(ctx, db.EventScope{BotID: 123, Organization: "org"}, args.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.EventChangeReceipt(ctx, scope, "rollback-event")
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.Pool().Exec(ctx, "DROP TRIGGER reject_event_result ON shuttle_event_change_proposals")
	require.NoError(t, err)
	_, err = h.store.ConfirmEventChange(ctx, scope, "rollback-event", args, prep.ID)
	require.NoError(t, err)
}

func TestEventChangeExpiryIncludesBlockedUpdateAndCreation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"update", "create"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			h, scope, args := eventChangeFixture(t)
			ctx := t.Context()
			eventScope := db.EventScope{BotID: 123, Organization: "org"}
			if mode == "update" {
				created, e := h.store.ChangeEvent(ctx, eventScope, scope.Subject, "base", args)
				require.NoError(t, e)
				args.ExpectedVersion = created.Version
			}
			prep, err := h.store.PrepareEventChange(ctx, scope, "blocked-event", args)
			require.NoError(t, err)
			_, err = h.store.Pool().
				Exec(ctx, "UPDATE shuttle_event_change_proposals SET expires_at=clock_timestamp()+interval '700 milliseconds'")
			require.NoError(t, err)
			holder, err := h.store.Pool().Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = holder.Rollback(context.Background()) })
			var pid int32
			if mode == "update" {
				require.NoError(
					t,
					holder.QueryRow(ctx, "SELECT pg_backend_pid() FROM shuttle_events WHERE id=$1 FOR UPDATE", args.ID).
						Scan(&pid),
				)
			} else {
				require.NoError(
					t,
					holder.QueryRow(ctx, `INSERT INTO shuttle_events(id,bot_id,organization_id,name,timezone,starts_at,ends_at,modules)
VALUES($1,123,'org','temporary','Europe/Minsk',$2,$3,ARRAY['registrations']) RETURNING pg_backend_pid()`, args.ID, args.Document.StartsAt, args.Document.EndsAt).
						Scan(&pid),
				)
			}
			result := make(chan error, 1)
			go func() {
				_, e := h.store.ConfirmEventChange(ctx, scope, "blocked-event", args, prep.ID)
				result <- e
			}()
			require.Eventually(t, func() bool {
				var blocked bool
				e := h.store.Pool().
					QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))", pid).
					Scan(&blocked)
				return e == nil && blocked
			}, 3*time.Second, 10*time.Millisecond)
			require.Eventually(t, func() bool {
				var expired bool
				e := h.store.Pool().
					QueryRow(ctx, "SELECT expires_at<=clock_timestamp() FROM shuttle_event_change_proposals").
					Scan(&expired)
				return e == nil && expired
			}, 3*time.Second, 10*time.Millisecond)
			require.NoError(t, holder.Rollback(ctx))
			require.ErrorIs(t, <-result, db.ErrConflict)
			if mode == "update" {
				current, e := h.store.Event(ctx, eventScope, args.ID)
				require.NoError(t, e)
				require.Equal(t, args.ExpectedVersion, current.Version)
			} else {
				_, e := h.store.Event(ctx, eventScope, args.ID)
				require.ErrorIs(t, e, pgx.ErrNoRows)
			}
		})
	}
}

func TestEventChangeMigrationPreservesProposalHistory(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	ctx := t.Context()
	driver := stdlib.OpenDBFromPool(h.store.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 31, false)
	require.NoError(t, err)
	// The previous operator path works while the additive proposal table is absent.
	created, err := h.store.ChangeEvent(
		ctx,
		db.EventScope{BotID: 123, Organization: "org"},
		scope.Subject,
		"old-path",
		args,
	)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 31, true)
	require.NoError(t, err)
	args.ExpectedVersion = created.Version
	prep, err := h.store.PrepareEventChange(ctx, scope, "retained-event", args)
	require.NoError(t, err)
	_, err = provider.ApplyVersion(ctx, 31, false)
	require.ErrorContains(t, err, "event approval and result history must be exported")
	repeated, err := h.store.PrepareEventChange(ctx, scope, "retained-event", args)
	require.NoError(t, err)
	sameTaskClosureJSON(t, prep, repeated)
}
