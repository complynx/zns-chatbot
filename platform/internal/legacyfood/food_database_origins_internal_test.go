package legacyfood

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood/dbgen"
)

// r31Row assigns scripted column values to Scan destinations in order; a nil
// value leaves the destination untouched, as a SQL NULL scanned into []byte does.
type r31Row struct {
	values []any
	err    error
}

func (r r31Row) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		if value != nil {
			reflect.ValueOf(dest[index]).Elem().Set(reflect.ValueOf(value))
		}
	}
	return nil
}

// r31Tx answers QueryRow from a script; Exec, Query and exhausted rows fail with err.
type r31Tx struct {
	failingTx

	rows *[]r31Row
}

func (t r31Tx) QueryRow(context.Context, string, ...any) pgx.Row {
	if len(*t.rows) == 0 {
		return failingRow{err: t.err}
	}
	row := (*t.rows)[0]
	*t.rows = (*t.rows)[1:]
	return row
}

func r31Script(err error, rows ...r31Row) r31Tx {
	return r31Tx{err: err, rows: &rows}
}

func r31Transport() error {
	return fmt.Errorf("private-transport-canary: %w", io.ErrUnexpectedEOF)
}

func r31RequireSafeDatabase(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, core.ErrDatabase.Error())
	assert.NotContains(t, err.Error(), "private-")
}

func r31RequireDataError(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	require.NotErrorIs(t, err, core.ErrDatabase)
	require.False(t, core.IsDatabaseFailure(err), "stored JSON rejected by the domain type is not a SQL failure")
}

func r31RequireProblem(t *testing.T, err error, code string) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, code, problem.Code)
	require.False(t, core.IsDatabaseFailure(err))
}

func r31DeliveryService(t *testing.T, completed int) Service {
	t.Helper()
	s := droppingService(t, completed)
	s.BotID = 77
	s.Delivery = delivery.Settings{
		BotID:        909090,
		BotInterval:  time.Millisecond,
		ChatInterval: time.Millisecond,
		Fallback:     30 * time.Second,
	}
	return s
}

// A connection dropped at a transaction start, the first statement or the first
// statement inside a transaction reaches callers only as the safe marker.
func TestR31FoodDroppedConnectionKeepsSQLProvenance(t *testing.T) {
	t.Parallel()
	consumed := delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_domain_notice_consumed"}
	for name, test := range map[string]struct {
		completed int
		call      func(context.Context, Service) error
	}{
		"owner capabilities": {0, func(ctx context.Context, s Service) error {
			_, err := s.OwnerCapabilities(ctx, "alice")
			return err
		}},
		"export begin": {0, func(ctx context.Context, s Service) error {
			_, err := s.Export(ctx, "bob", "food-event")
			return err
		}},
		"execute begin": {0, func(ctx context.Context, s Service) error {
			_, err := s.Execute(ctx, "alice", Command{EventID: "food-event", Name: commandSaveMeals, Key: "r31"})
			return err
		}},
		"execute event lock": {1, func(ctx context.Context, s Service) error {
			_, err := s.Execute(ctx, "alice", Command{EventID: "food-event", Name: commandSaveMeals, Key: "r31"})
			return err
		}},
		"legacy menu begin": {0, func(ctx context.Context, s Service) error {
			_, err := s.SaveLegacyMenu(ctx, "alice", "food-event", strings.Repeat("a", 64), MealSelection{})
			return err
		}},
		"notification status": {0, func(ctx context.Context, s Service) error {
			_, err := s.NotificationStatus(ctx, 1)
			return err
		}},
		"prepare notification index": {0, func(ctx context.Context, s Service) error {
			_, _, err := s.PrepareNotification(ctx, 1)
			return err
		}},
		"pending recovery index": {0, func(ctx context.Context, s Service) error {
			_, err := s.PendingNotifications(ctx)
			return err
		}},
		"begin notification": {0, func(ctx context.Context, s Service) error {
			_, err := s.BeginNotification(ctx, delivery.Attempt{ID: 1, Generation: 1})
			return err
		}},
		"begin notification eligibility": {1, func(ctx context.Context, s Service) error {
			_, err := s.BeginNotification(ctx, delivery.Attempt{ID: 1, Generation: 1})
			return err
		}},
		"complete notification": {0, func(ctx context.Context, s Service) error {
			return s.CompleteNotification(ctx, NotificationCompletion{ID: 1, Attempt: 1, Outcome: consumed})
		}},
		"complete notification attempt lock": {1, func(ctx context.Context, s Service) error {
			return s.CompleteNotification(ctx, NotificationCompletion{ID: 1, Attempt: 1, Outcome: consumed})
		}},
		"complete followup": {0, func(ctx context.Context, s Service) error {
			return s.CompleteNotificationFollowup(ctx, NotificationFollowup{ID: 1, Attempt: 1, Done: true})
		}},
		"complete followup attempt lock": {1, func(ctx context.Context, s Service) error {
			return s.CompleteNotificationFollowup(ctx, NotificationFollowup{ID: 1, Attempt: 1, Done: true})
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), reminderTestTimeout)
			defer cancel()
			r31RequireSafeDatabase(t, test.call(ctx, r31DeliveryService(t, test.completed)))
		})
	}
}

// Statement faults are sanitized after the expected absence mapping; absence,
// cancellation and serialization identity survive.
func TestR31FoodStatementFaultsKeepDomainMappings(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := Service{BotID: 77, Delivery: delivery.Settings{BotID: 909090}}
	proof := DeliveryRead{Event: "food-event", Order: "o", Kind: Meals, Generation: 1}

	r31RequireSafeDatabase(t, lockDeliveryProofInTx(ctx, failingTx{err: r31Transport()}, "alice", proof))
	err := lockDeliveryProofInTx(ctx, failingTx{err: pgx.ErrNoRows}, "alice", proof)
	r31RequireProblem(t, err, "food_proof_unavailable")
	require.ErrorIs(t, lockDeliveryProofInTx(ctx, failingTx{err: context.Canceled}, "alice", proof), context.Canceled)
	serialization := &pgconn.PgError{Code: "40001", Message: "private-serialization"}
	err = lockDeliveryProofInTx(ctx, failingTx{err: serialization}, "alice", proof)
	require.ErrorIs(t, err, core.ErrDatabaseSerialization)
	assert.NotContains(t, err.Error(), "private-")

	_, err = s.event(ctx, failingTx{err: pgx.ErrNoRows}, "food-event", true)
	r31RequireProblem(t, err, "food_event_unavailable")
	_, err = s.event(ctx, failingTx{err: r31Transport()}, "food-event", true)
	r31RequireSafeDatabase(t, err)

	_, err = loadOrder(ctx, failingTx{err: pgx.ErrNoRows}, "food-event", "", "alice")
	require.True(t, noOrder(err), "absence still selects the empty view or first create")
	_, err = loadOrder(ctx, failingTx{err: r31Transport()}, "food-event", "", "alice")
	r31RequireSafeDatabase(t, err)
	require.False(t, noOrder(err), "a database failure never becomes an absent order")

	absentPayment, err := loadPayment(ctx, failingTx{err: pgx.ErrNoRows}, "o", Activity)
	require.NoError(t, err)
	assert.Equal(t, Payment{Kind: Activity, Status: Pending}, absentPayment)
	_, err = loadPayment(ctx, failingTx{err: r31Transport()}, "o", Activity)
	r31RequireSafeDatabase(t, err)

	receiver := &operation{tx: failingTx{err: pgx.ErrNoRows}, event: Event{Active: true}}
	r31RequireProblem(t, receiver.assignReceiver(ctx, &Order{EventID: "food-event"}), "food_payment_admin_unavailable")
	receiver.tx = failingTx{err: r31Transport()}
	r31RequireSafeDatabase(t, receiver.assignReceiver(ctx, &Order{EventID: "food-event"}))
	r31RequireSafeDatabase(t, receiver.toggle(ctx, &Order{EventID: "food-event"}))
	r31RequireSafeDatabase(t, receiver.save(ctx, Order{ID: "o", EventID: "food-event"}))

	notice := &operation{tx: failingTx{err: pgx.ErrNoRows}, service: s}
	order := &Order{ID: "o", EventID: "food-event"}
	require.NoError(t, notice.notice(ctx, order, "alice", Paid, Payment{Kind: Meals, Generation: 1}))
	assert.Empty(t, notice.notificationRegistrations, "a deduplicated notice registers no delivery")
	notice.tx = failingTx{err: r31Transport()}
	r31RequireSafeDatabase(t, notice.notice(ctx, order, "alice", Paid, Payment{Kind: Meals, Generation: 1}))

	_, err = s.lockNotificationEligibility(ctx, failingTx{err: pgx.ErrNoRows}, 1)
	r31RequireProblem(t, notificationAttemptError(err), "notification_stale_attempt")
	_, err = s.lockNotificationEligibility(ctx, failingTx{err: r31Transport()}, 1)
	r31RequireSafeDatabase(t, notificationAttemptError(err))

	attempt := delivery.Attempt{ID: 1, Generation: 1}
	outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_domain_notice_consumed"}
	failing := dbgen.New(failingTx{err: r31Transport()})
	r31RequireSafeDatabase(t, s.saveNotificationOutcome(ctx, failing, attempt, outcome, "", time.Time{}, 0))

	legacy := Payment{Kind: Meals, Status: Paid, LegacySourceKey: "legacy"}
	_, err = aggregatePayment(ctx, failingTx{err: r31Transport()}, legacy)
	r31RequireSafeDatabase(t, err)
}

// Stored JSON is read as raw bytes and decoded separately, so a value the
// domain type rejects keeps a data error while SQL faults keep the marker.
func TestR31FoodStoredJSONIsNotDatabaseFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := Service{BotID: 77}
	orderRow := func(meals, activities []byte) r31Row {
		return r31Row{values: []any{"o", "food-event", "alice", int64(2), meals, nil, nil, activities}}
	}

	negative := orderRow([]byte(`{"friday":{"dinner":[-1]}}`), nil)
	_, err := loadOrder(ctx, r31Script(r31Transport(), negative), "food-event", "o", "")
	r31RequireDataError(t, err)
	_, err = loadOrder(ctx, r31Script(r31Transport(), orderRow(nil, []byte(`["open"]`))), "food-event", "o", "")
	r31RequireDataError(t, err)

	absent := r31Row{err: pgx.ErrNoRows}
	order, err := loadOrder(
		ctx,
		r31Script(
			r31Transport(),
			orderRow([]byte(`{"friday":{"dinner":["0"]}}`), []byte(`{"open":true}`)),
			absent,
			absent,
		),
		"food-event",
		"o",
		"",
	)
	require.NoError(t, err)
	assert.Equal(t, []Index{0}, order.Meals["friday"].Dinner)
	assert.Equal(t, Activities{"open": true}, order.Activities)
	assert.Equal(t, Payment{Kind: Meals, Status: Pending}, order.MealPayment)
	assert.Equal(t, Payment{Kind: Activity, Status: Pending}, order.ActivityPayment)

	order, err = loadOrder(ctx, r31Script(r31Transport(), orderRow(nil, nil), absent, absent), "food-event", "o", "")
	require.NoError(t, err, "SQL NULL keeps the pgx nil-map result")
	assert.Nil(t, order.Meals)
	assert.Nil(t, order.Activities)

	_, err = loadOrder(ctx, r31Script(r31Transport(), orderRow(nil, nil)), "food-event", "o", "")
	r31RequireSafeDatabase(t, err)

	eventRow := func(prices []byte) r31Row {
		return r31Row{values: []any{"food-event", json.RawMessage(`{}`), "sha", prices, []byte(`{}`)}}
	}
	_, err = s.event(ctx, r31Script(r31Transport(), eventRow([]byte(`[]`))), "food-event", true)
	r31RequireDataError(t, err)
	var typeErr *json.UnmarshalTypeError
	require.ErrorAs(t, err, &typeErr)
	_, err = s.event(ctx, r31Script(r31Transport(), eventRow(nil)), "food-event", true)
	r31RequireDataError(t, err)
	stored := []byte(`{"with_soup":665}`)
	var want MealPrices
	require.NoError(t, json.Unmarshal(stored, &want))
	event, err := s.event(ctx, r31Script(r31Transport(), eventRow(stored)), "food-event", true)
	require.NoError(t, err)
	assert.NotZero(t, event.MealPrices.WithSoup)
	assert.Equal(t, want, event.MealPrices)
}

func TestR31FoodReplayReceiptDecodesAfterSQL(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := Service{BotID: 77}
	replayCommand := Command{EventID: "food-event", Name: commandSaveMeals, Key: "r31-replay"}
	raw, err := json.Marshal(replayCommand)
	require.NoError(t, err)
	hash := digest(raw)
	prepare := func(receipt r31Row) (PreparedCommand, error) {
		tx := r31Script(
			r31Transport(),
			r31Row{values: []any{"food-event", json.RawMessage(`{}`), "sha", []byte(`{}`), []byte(`{}`)}},
			r31Row{values: []any{true}},
			r31Row{values: []any{time.Now()}},
			receipt,
		)
		return s.PrepareInTx(ctx, tx, "alice", replayCommand)
	}

	prepared, err := prepare(r31Row{values: []any{hash, []byte(`{"id":"o","version":3}`)}})
	require.NoError(t, err)
	replayed, found := prepared.Replay()
	require.True(t, found)
	assert.Equal(t, "o", replayed.ID)
	assert.Equal(t, int64(3), replayed.Version)

	_, err = prepare(r31Row{values: []any{hash, []byte(`{"meals":{"friday":{"dinner":["x"]}}}`)}})
	r31RequireDataError(t, err)

	_, err = prepare(r31Row{values: []any{"other", []byte(`{"id":"o"}`)}})
	r31RequireProblem(t, err, "idempotency_conflict")

	prepared, err = prepare(r31Row{err: pgx.ErrNoRows})
	require.NoError(t, err)
	_, found = prepared.Replay()
	require.False(t, found, "absent receipt executes the command")

	_, err = prepare(r31Row{err: r31Transport()})
	r31RequireSafeDatabase(t, err)
	_, err = prepare(r31Row{err: context.Canceled})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestR31DecodeStoredJSONMatchesPgxScan(t *testing.T) {
	t.Parallel()
	activities := Activities{"yoga": true}
	require.NoError(t, decodeStoredJSON([]byte(`{"open":true}`), &activities))
	assert.Equal(t, Activities{"open": true}, activities, "the destination is zeroed before decoding")
	require.NoError(t, decodeStoredJSON(nil, &activities))
	assert.Nil(t, activities, "SQL NULL zeroes a map")
	prices := MealPrices{WithSoup: 1}
	err := decodeStoredJSON(nil, &prices)
	require.Error(t, err, "SQL NULL cannot fill a struct")
	require.False(t, core.IsDatabaseFailure(err))
	require.NotErrorIs(t, err, pgx.ErrNoRows)
}
