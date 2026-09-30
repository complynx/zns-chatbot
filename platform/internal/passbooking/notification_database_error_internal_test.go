package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

var errUnexpectedQuery = errors.New("fake cannot return query rows")

// notificationSQLTx fails exactly one SQL call; other calls succeed with zero rows.
type notificationSQLTx struct {
	pgx.Tx

	err    error
	tag    string
	bytes  string
	failAt int
	calls  int
}

func (tx *notificationSQLTx) next() error {
	tx.calls++
	if tx.calls == tx.failAt {
		return tx.err
	}
	return nil
}

func (tx *notificationSQLTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tag := tx.tag
	if tag == "" {
		tag = "UPDATE 1"
	}
	return pgconn.NewCommandTag(tag), tx.next()
}

func (tx *notificationSQLTx) QueryRow(context.Context, string, ...any) pgx.Row {
	bytes := tx.bytes
	if bytes == "" {
		bytes = "{}"
	}
	return notificationSQLRow{err: tx.next(), bytes: bytes}
}

func (tx *notificationSQLTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if err := tx.next(); err != nil {
		return nil, err
	}
	return nil, errUnexpectedQuery
}

type notificationSQLRow struct {
	err   error
	bytes string
}

func (row notificationSQLRow) Scan(dest ...any) error {
	if row.err != nil {
		return row.err
	}
	for _, value := range dest {
		if target, ok := value.(*[]byte); ok {
			*target = []byte(row.bytes)
		}
	}
	return nil
}

func requireSanitizedDatabaseError(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, core.ErrDatabase)
	require.EqualError(t, err, "database unavailable")
	require.NotErrorIs(t, err, io.EOF)
	var driver *pgconn.PgError
	require.NotErrorAs(t, err, &driver)
}

func requireProblemCode(t *testing.T, err error, code string) {
	t.Helper()
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, code, problem.Code)
	require.NotErrorIs(t, err, core.ErrDatabase)
}

func notificationRow(payload string) dbgen.CorePassNotification {
	return dbgen.CorePassNotification{ID: 1, EventID: "event", Owner: "owner", Recipient: "owner",
		Payload: []byte(payload)}
}

func TestNotificationSQLOriginErrors(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	assignedAt := now.Add(-7 * 24 * time.Hour)
	firstAt := now.Add(-25 * time.Hour)
	reminded := func() *Booking {
		return &Booking{Event: "event", Owner: "owner", State: assigned, AssignedAt: &assignedAt}
	}
	attempt := delivery.Attempt{ID: 1, Generation: 1}
	cases := []struct {
		name  string
		calls int
		run   func(context.Context, pgx.Tx) error
	}{
		{"known_invitee_enqueue_pause", 3, func(ctx context.Context, tx pgx.Tx) error {
			var pending []delivery.Registration
			return notifyKnownInvitee(ctx, tx, 1, &pending, &Booking{}, 5, "invitation")
		}},
		{"enqueue_pause", 2, func(ctx context.Context, tx pgx.Tx) error {
			var pending []delivery.Registration
			return enqueuePassNotice(ctx, tx, 1, &pending, &Booking{}, "owner", "registered", "1", "")
		}},
		{"projection_titles_recipient_live", 3, func(ctx context.Context, tx pgx.Tx) error {
			_, err := Service{}.notificationProjection(ctx, tx, notificationRow("{}"))
			return err
		}},
		{"live_passport", 1, func(ctx context.Context, tx pgx.Tx) error {
			_, err := Service{}.livePassportReminder(ctx, tx, Notification{Owner: "owner"})
			return err
		}},
		{"eligibility_through_attempt_error", 5, func(ctx context.Context, tx pgx.Tx) error {
			_, err := Service{}.lockNotificationEligibility(ctx, tx, 1)
			return notificationAttemptError(err)
		}},
		{"save_notification_outcome", 1, func(ctx context.Context, tx pgx.Tx) error {
			outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "notification_no_longer_current"}
			return Service{}.saveNotificationOutcome(ctx, dbgen.New(tx), attempt, outcome, "", time.Time{}, 0)
		}},
		{"finish_announcement", 1, func(ctx context.Context, tx pgx.Tx) error {
			outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "announcement_superseded"}
			return Service{}.finishAnnouncement(ctx, dbgen.New(tx), attempt, outcome, now)
		}},
		{"announcement_admission", 4, func(ctx context.Context, tx pgx.Tx) error {
			_, err := Service{}.lockAnnouncementAdmission(ctx, dbgen.New(tx), attempt)
			return err
		}},
		{"announcement_source", 3, func(ctx context.Context, tx pgx.Tx) error {
			return Service{}.lockAnnouncementSource(ctx, tx, 1)
		}},
		{"enqueue_announcements", 1, func(ctx context.Context, tx pgx.Tx) error {
			_, err := enqueueRegistrationAnnouncements(ctx, tx, "event", 1, nil)
			return err
		}},
		{"deadline_markers", 1, func(ctx context.Context, tx pgx.Tx) error {
			_, err := readDeadlineMarkers(ctx, tx, "event")
			return err
		}},
		{"first_reminder", 3, func(ctx context.Context, tx pgx.Tx) error {
			_, err := (&snapshot{now: now}).processDeadline(ctx, tx, reminded(), deadlineMarker{Owner: "owner"})
			return err
		}},
		{"second_reminder", 3, func(ctx context.Context, tx pgx.Tx) error {
			marker := deadlineMarker{Owner: "owner", First: &firstAt}
			_, err := (&snapshot{now: now}).processDeadline(ctx, tx, reminded(), marker)
			return err
		}},
	}
	faults := []error{io.EOF, &pgconn.PgError{Code: "P0001", Message: "private diagnostic"}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for step := 1; step <= test.calls; step++ {
				for _, fault := range faults {
					tx := &notificationSQLTx{failAt: step, err: fault}
					err := test.run(t.Context(), tx)
					requireSanitizedDatabaseError(t, err)
					require.NotContains(t, err.Error(), "private")
					require.Equal(t, step, tx.calls, "SQL step %d", step)
				}
				for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
					tx := &notificationSQLTx{failAt: step, err: cancellation}
					err := test.run(t.Context(), tx)
					require.ErrorIs(t, err, cancellation, "SQL step %d", step)
					require.NotErrorIs(t, err, core.ErrDatabase)
					require.Equal(t, step, tx.calls)
				}
			}
		})
	}
}

func TestNotificationExpectedOutcomesSurviveTagging(t *testing.T) {
	t.Parallel()
	attempt := delivery.Attempt{ID: 1, Generation: 1}

	t.Run("unknown_invitee_is_noop", func(t *testing.T) {
		t.Parallel()
		var pending []delivery.Registration
		tx := &notificationSQLTx{failAt: 1, err: pgx.ErrNoRows}
		require.NoError(t, notifyKnownInvitee(t.Context(), tx, 1, &pending, &Booking{}, 5, "invitation"))
		require.Equal(t, 1, tx.calls)
		require.Empty(t, pending)
	})

	t.Run("duplicate_enqueue_is_noop", func(t *testing.T) {
		t.Parallel()
		var pending []delivery.Registration
		tx := &notificationSQLTx{failAt: 1, err: pgx.ErrNoRows}
		require.NoError(t, enqueuePassNotice(t.Context(), tx, 1, &pending, &Booking{}, "owner", "registered", "1", ""))
		require.Equal(t, 1, tx.calls)
		require.Empty(t, pending)
	})

	t.Run("payload_json_is_not_database", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{}
		_, err := Service{}.notificationProjection(t.Context(), tx, notificationRow("{"))
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
		require.NotErrorIs(t, err, core.ErrDatabase)
		require.Zero(t, tx.calls)
	})

	t.Run("titles_json_is_not_database", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{bytes: "{"}
		_, err := Service{}.notificationProjection(t.Context(), tx, notificationRow("{}"))
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
		require.NotErrorIs(t, err, core.ErrDatabase)
		require.Equal(t, 1, tx.calls)
	})

	t.Run("titles_absence_is_preserved", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{failAt: 1, err: pgx.ErrNoRows}
		_, err := Service{}.notificationProjection(t.Context(), tx, notificationRow("{}"))
		require.ErrorIs(t, err, pgx.ErrNoRows)
		require.NotErrorIs(t, err, core.ErrDatabase)
		require.Equal(t, 1, tx.calls)
	})

	t.Run("eligibility_absence_is_stale", func(t *testing.T) {
		t.Parallel()
		for step := 1; step <= 2; step++ {
			tx := &notificationSQLTx{failAt: step, err: pgx.ErrNoRows}
			_, err := Service{}.lockNotificationEligibility(t.Context(), tx, 1)
			requireProblemCode(t, notificationAttemptError(err), "notification_stale_attempt")
			require.Equal(t, step, tx.calls)
		}
	})

	t.Run("eligibility_json_keeps_mixed_helper_error", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{bytes: "{"}
		_, err := Service{}.lockNotificationEligibility(t.Context(), tx, 1)
		err = notificationAttemptError(err)
		var syntax *json.SyntaxError
		require.ErrorAs(t, err, &syntax)
		require.NotErrorIs(t, err, core.ErrDatabase)
		require.Equal(t, 2, tx.calls)
	})

	t.Run("notification_count_stale", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{tag: "UPDATE 0"}
		outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "notification_no_longer_current"}
		err := Service{}.saveNotificationOutcome(t.Context(), dbgen.New(tx), attempt, outcome, "", time.Time{}, 0)
		requireProblemCode(t, err, "notification_stale_attempt")
		require.Equal(t, 1, tx.calls)
		tx = &notificationSQLTx{}
		require.NoError(t, Service{}.saveNotificationOutcome(
			t.Context(), dbgen.New(tx), attempt, outcome, "", time.Time{}, 0))
		require.Equal(t, 1, tx.calls)
	})

	t.Run("announcement_count_stale", func(t *testing.T) {
		t.Parallel()
		tx := &notificationSQLTx{tag: "UPDATE 0"}
		outcome := delivery.Outcome{Kind: delivery.Cancelled, Reason: "announcement_superseded"}
		err := Service{}.finishAnnouncement(t.Context(), dbgen.New(tx), attempt, outcome, time.Time{})
		requireProblemCode(t, err, "pass_announcement_stale")
		require.Equal(t, 1, tx.calls)
	})

	t.Run("announcement_admission_absence_is_stale", func(t *testing.T) {
		t.Parallel()
		for _, step := range []int{1, 4} {
			tx := &notificationSQLTx{failAt: step, err: pgx.ErrNoRows}
			_, err := Service{}.lockAnnouncementAdmission(t.Context(), dbgen.New(tx), attempt)
			requireProblemCode(t, err, "pass_announcement_stale")
			require.Equal(t, step, tx.calls)
		}
	})

	t.Run("announcement_source_locks_tolerate_absence", func(t *testing.T) {
		t.Parallel()
		for _, step := range []int{2, 3} {
			tx := &notificationSQLTx{failAt: step, err: pgx.ErrNoRows}
			require.NoError(t, Service{}.lockAnnouncementSource(t.Context(), tx, 1))
			require.Equal(t, 3, tx.calls)
		}
		tx := &notificationSQLTx{failAt: 1, err: pgx.ErrNoRows}
		require.ErrorIs(t, Service{}.lockAnnouncementSource(t.Context(), tx, 1), pgx.ErrNoRows)
		require.Equal(t, 1, tx.calls)
	})
}
